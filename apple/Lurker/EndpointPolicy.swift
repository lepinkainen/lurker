import Foundation

// MARK: - EndpointPolicyError

enum EndpointPolicyError: LocalizedError, Equatable {
  case invalidURL
  case insecureLocation
  case unsupportedComponents

  var errorDescription: String? {
    switch self {
    case .invalidURL: "Enter a valid HTTP URL."
    case .insecureLocation: "Use localhost, a Tailnet IP, or a MagicDNS name."
    case .unsupportedComponents:
      "The server URL cannot contain credentials, a path, query, or fragment."
    }
  }
}

// MARK: - EndpointPolicy

enum EndpointPolicy {

  // MARK: Internal

  static func normalize(_ value: String) throws -> URL {
    let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard
      var components = URLComponents(string: trimmed),
      components.scheme?.lowercased() == "http",
      let rawHost = components.host?.lowercased(),
      !rawHost.isEmpty
    else {
      throw EndpointPolicyError.invalidURL
    }
    let host =
      rawHost.hasPrefix("[") && rawHost.hasSuffix("]")
        ? String(rawHost.dropFirst().dropLast())
        : rawHost
    guard
      components.user == nil,
      components.password == nil,
      components.query == nil,
      components.fragment == nil,
      components.path.isEmpty || components.path == "/"
    else {
      throw EndpointPolicyError.unsupportedComponents
    }
    guard isPrivateHost(host) else {
      throw EndpointPolicyError.insecureLocation
    }
    components.scheme = "http"
    components.host = rawHost
    components.path = ""
    guard let url = components.url else {
      throw EndpointPolicyError.invalidURL
    }
    return url
  }

  static func allows(_ url: URL) -> Bool {
    (try? normalize(url.absoluteString)) != nil
  }

  static func isPrivateHost(_ host: String) -> Bool {
    if host == "localhost" || host == "::1" || host.hasSuffix(".localhost") {
      return true
    }
    if host.hasSuffix(".ts.net") {
      return true
    }
    if !host.contains("."), isValidSingleLabel(host) {
      return true
    }
    guard let octets = ipv4Octets(host) else {
      return false
    }
    if octets[0] == 127 {
      return true
    }
    return octets[0] == 100 && (64...127).contains(octets[1])
  }

  // MARK: Private

  private static func isValidSingleLabel(_ host: String) -> Bool {
    guard
      !host.isEmpty, host.count <= 63,
      host.first != "-", host.last != "-"
    else {
      return false
    }
    return host.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-") }
  }

  private static func ipv4Octets(_ host: String) -> [Int]? {
    let parts = host.split(separator: ".", omittingEmptySubsequences: false)
    guard parts.count == 4 else { return nil }
    let values = parts.compactMap { part -> Int? in
      guard !part.isEmpty, part.allSatisfy(\.isNumber), let value = Int(part), value <= 255 else {
        return nil
      }
      return value
    }
    return values.count == 4 ? values : nil
  }

}
