import Foundation

enum LurkerAPIError: LocalizedError, Sendable {
  case invalidResponse
  case httpStatus(Int, String)
  case wrongService
  case untrustedPath
  case disconnected
  case textExpected

  var errorDescription: String? {
    switch self {
    case .invalidResponse: "The server returned an invalid response."
    case .httpStatus(let code, let message):
      message.isEmpty ? "Server returned HTTP \(code)." : message
    case .wrongService: "This address does not identify a Lurker server."
    case .untrustedPath: "The server connection is not local or routed through Tailscale."
    case .disconnected: "The live connection is not ready."
    case .textExpected: "The server returned an unsupported WebSocket frame."
    }
  }
}

struct ClientCommand: Encodable, Sendable, Equatable {
  var type: String
  var reqID: String = UUID().uuidString
  var bufferID: UUID?
  var networkID: UUID?
  var channel: String?
  var target: String?
  var content: String?
  var before: UUID?
  var limit: Int?
  var messageID: UUID?
}

protocol LurkerTransport: Sendable {
  func validateServer() async throws -> ServiceIdentity
  func fetchState() async throws -> StateSnapshot
  func fetchHistory(bufferID: UUID, before: UUID?) async throws -> [Message]
  func updateBuffer(id: UUID, patch: BufferSettingsPatch) async throws -> BufferSettingsEvent
  func openEvents() async -> AsyncThrowingStream<ServerEvent, Error>
  func send(_ command: ClientCommand) async throws
  func disconnect() async
}

private final class RedirectGuard: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
  func urlSession(
    _: URLSession,
    task _: URLSessionTask,
    willPerformHTTPRedirection _: HTTPURLResponse,
    newRequest request: URLRequest,
    completionHandler: @escaping (URLRequest?) -> Void
  ) {
    guard let url = request.url, EndpointPolicy.allows(url) else {
      completionHandler(nil)
      return
    }
    completionHandler(request)
  }
}

actor LurkerAPI: LurkerTransport {
  let baseURL: URL
  private let session: URLSession
  private var socket: URLSessionWebSocketTask?
  private let decoder = JSONDecoder.lurker()
  private let encoder = JSONEncoder.lurker()

  init(baseURL: URL) {
    self.baseURL = baseURL
    let configuration = URLSessionConfiguration.ephemeral
    configuration.timeoutIntervalForRequest = 15
    configuration.timeoutIntervalForResource = 30
    configuration.waitsForConnectivity = true
    session = URLSession(
      configuration: configuration, delegate: RedirectGuard(), delegateQueue: nil)
  }

  func validateServer() async throws -> ServiceIdentity {
    let identity: ServiceIdentity = try await get("whoami")
    guard identity.name.lowercased() == "lurker" else {
      throw LurkerAPIError.wrongService
    }
    let path: TailscaleStatus = try await get("api/tailscale-status")
    guard path.status == "local" || path.status == "tailscale" else {
      throw LurkerAPIError.untrustedPath
    }
    return identity
  }

  func fetchState() async throws -> StateSnapshot {
    try await get("api/state")
  }

  func fetchHistory(bufferID: UUID, before: UUID?) async throws -> [Message] {
    var components = URLComponents(
      url: url("api/buffers/\(bufferID.uuidString)/history"), resolvingAgainstBaseURL: false)
    components?.queryItems = [
      before.map { URLQueryItem(name: "before", value: $0.uuidString) },
      URLQueryItem(name: "limit", value: "200"),
    ].compactMap(\.self)
    guard let historyURL = components?.url else {
      throw LurkerAPIError.invalidResponse
    }
    let result: HistoryResult = try await request(historyURL)
    return result.messages
  }

  func updateBuffer(id: UUID, patch: BufferSettingsPatch) async throws -> BufferSettingsEvent {
    var request = URLRequest(url: url("api/buffers/\(id.uuidString)/settings"))
    request.httpMethod = "PATCH"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    request.httpBody = try encoder.encode(patch)
    return try await requestJSON(request)
  }

  func openEvents() -> AsyncThrowingStream<ServerEvent, Error> {
    socket?.cancel(with: .goingAway, reason: nil)
    var components = URLComponents(url: url("api/stream"), resolvingAgainstBaseURL: false)!
    components.scheme = "ws"
    let task = session.webSocketTask(with: components.url!)
    socket = task
    task.resume()
    return AsyncThrowingStream { continuation in
      let reader = Task {
        do {
          while !Task.isCancelled {
            let frame = try await task.receive()
            let data: Data
            switch frame {
            case .data(let value): data = value
            case .string(let value):
              guard let value = value.data(using: .utf8) else {
                throw LurkerAPIError.textExpected
              }
              data = value
            @unknown default:
              throw LurkerAPIError.textExpected
            }
            continuation.yield(try self.decoder.decode(ServerEvent.self, from: data))
          }
          continuation.finish()
        } catch is CancellationError {
          continuation.finish()
        } catch {
          continuation.finish(throwing: error)
        }
      }
      continuation.onTermination = { _ in
        reader.cancel()
        task.cancel(with: .goingAway, reason: nil)
      }
    }
  }

  func send(_ command: ClientCommand) async throws {
    guard let socket else {
      throw LurkerAPIError.disconnected
    }
    let data = try encoder.encode(command)
    guard let text = String(data: data, encoding: .utf8) else {
      throw LurkerAPIError.textExpected
    }
    try await socket.send(.string(text))
  }

  func disconnect() {
    socket?.cancel(with: .goingAway, reason: nil)
    socket = nil
  }

  private func get<T: Decodable>(_ path: String) async throws -> T {
    try await request(url(path))
  }

  private func request<T: Decodable>(_ target: URL) async throws -> T {
    var request = URLRequest(url: target)
    request.setValue("application/json", forHTTPHeaderField: "Accept")
    return try await requestJSON(request)
  }

  private func requestJSON<T: Decodable>(_ request: URLRequest) async throws -> T {
    let (data, response) = try await session.data(for: request)
    guard let response = response as? HTTPURLResponse else {
      throw LurkerAPIError.invalidResponse
    }
    guard (200..<300).contains(response.statusCode) else {
      throw LurkerAPIError.httpStatus(
        response.statusCode, String(data: data, encoding: .utf8) ?? "")
    }
    return try decoder.decode(T.self, from: data)
  }

  private func url(_ path: String) -> URL {
    baseURL.appending(path: path)
  }
}
