import Foundation

#if DEBUG
@MainActor
extension AppModel {
  /// A fully hydrated, "connected and joined" model for SwiftUI previews.
  /// Populates state synchronously instead of running the async connection loop,
  /// so previews render fully populated in a single pass with no transport,
  /// async work, or mark-read side effects.
  static func preview() -> AppModel {
    let model = AppModel(transport: FixtureTransport(), runsConnectionLoop: false)
    model.applySnapshot(FixtureTransport.snapshot())
    model.serviceIdentity = FixtureTransport.identity
    model.connectionState = .connected
    model.hydrated = true
    model.selectedBufferID = FixtureTransport.channelID
    return model
  }

  /// A multi-network fixture for the sidebar preview: several servers, each with
  /// a status buffer plus a handful of channels/queries carrying varied unread and
  /// mention counts. Hand-built here (not via `FixtureTransport`) so it can grow
  /// without disturbing the UI-test fixture.
  static func previewSidebar() -> AppModel {
    var networks = [Network]()
    var buffers = [Buffer]()
    var firstChannelID: UUID?

    func addNetwork(
      _ name: String,
      sort: Int,
      status: String,
      channels: [(name: String, unread: Int, mentions: Int, joined: Bool)],
      queries: [String] = [],
    ) {
      // Parted channels double as archived fixtures (server archives on part).
      let netID = UUID()
      networks.append(
        Network(
          id: netID,
          name: name,
          kind: "irc",
          host: "irc.\(name.lowercased()).net",
          port: 6697,
          tls: true,
          nick: "shrike",
          status: status,
          sortOrder: sort,
        )
      )
      buffers.append(
        Buffer(
          id: UUID(),
          networkID: netID,
          name: name,
          kind: "status",
          joined: true,
          showEmbeds: false,
          showPresenceEvents: true,
          collapsePresenceEvents: false,
          pinned: false,
          unread: 0,
          mentions: 0,
        )
      )
      for channel in channels {
        let id = UUID()
        if firstChannelID == nil {
          firstChannelID = id
        }
        buffers.append(
          Buffer(
            id: id,
            networkID: netID,
            name: channel.name,
            kind: "channel",
            joined: channel.joined,
            showEmbeds: true,
            showPresenceEvents: true,
            collapsePresenceEvents: true,
            pinned: false,
            archived: !channel.joined,
            unread: channel.unread,
            mentions: channel.mentions,
          )
        )
      }
      for query in queries {
        buffers.append(
          Buffer(
            id: UUID(),
            networkID: netID,
            name: query,
            kind: "query",
            joined: true,
            showEmbeds: true,
            showPresenceEvents: true,
            collapsePresenceEvents: false,
            pinned: false,
            unread: 0,
            mentions: 0,
          )
        )
      }
    }

    addNetwork(
      "Libera",
      sort: 0,
      status: "connected",
      channels: [
        (name: "#general", unread: 0, mentions: 0, joined: true),
        (name: "#dev", unread: 3, mentions: 0, joined: true),
        (name: "#swift", unread: 0, mentions: 0, joined: true),
      ],
      queries: ["tove"],
    )
    addNetwork(
      "OFTC",
      sort: 1,
      status: "connected",
      channels: [
        (name: "#tor", unread: 12, mentions: 2, joined: true),
        (name: "#debian", unread: 0, mentions: 0, joined: true),
      ],
    )
    addNetwork(
      "Rizon",
      sort: 2,
      status: "connecting",
      channels: [
        (name: "#anime", unread: 99, mentions: 5, joined: true),
        (name: "#help", unread: 0, mentions: 0, joined: false),
      ],
    )

    let model = AppModel(transport: FixtureTransport(), runsConnectionLoop: false)
    model.applySnapshot(
      StateSnapshot(networks: networks, buffers: buffers, initialMessages: [:], members: nil)
    )
    model.serviceIdentity = FixtureTransport.identity
    model.connectionState = .connected
    model.hydrated = true
    model.selectedBufferID = firstChannelID
    return model
  }
}
#endif

extension ProcessInfo {
  static var isPreview: Bool {
    isPreviewEnvironment(processInfo.environment)
  }

  static var isUITest: Bool {
    processInfo.arguments.contains("-ui-testing")
  }

  /// True in Xcode Previews or UI tests, where AppKit/UserNotifications APIs
  /// (`UNUserNotificationCenter.current()`, `NSApp.dockTile`) crash or misbehave.
  static var isPreviewOrUITest: Bool {
    isPreview || isUITest
  }

  static func isPreviewEnvironment(_ environment: [String: String]) -> Bool {
    environment["XCODE_RUNNING_FOR_PREVIEWS"] == "1"
      || environment["XCODE_RUNNING_FOR_PLAYGROUNDS"] == "1"
  }
}
