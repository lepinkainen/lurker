import Foundation
import Testing

@testable import Lurker

@Suite
@MainActor
struct AppModelTests {
  @Test func sidebarAndKeyboardNavigationShareOneOrder() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let networkID = UUID()
    let status = buffer("*status*", kind: "status", networkID: networkID)
    let pinned = buffer("#zeta", networkID: networkID, pinned: true)
    let joined = buffer("#alpha", networkID: networkID)
    let query = buffer("bob", kind: "query", networkID: networkID)
    let notJoined = buffer("#old", networkID: networkID, joined: false)

    model.networks[networkID] = network(id: networkID)
    model.buffers = Dictionary(
      uniqueKeysWithValues: [status, pinned, joined, query, notJoined].map { ($0.id, $0) })

    #expect(model.pinnedBuffers.map(\.id) == [pinned.id])
    let groups = model.sidebarBuffers(for: networkID)
    #expect(groups.all.map(\.id) == [status.id, joined.id, notJoined.id, query.id])

    model.selectedBufferID = status.id
    model.nextBuffer()
    #expect(model.selectedBufferID == joined.id)
    model.nextBuffer()
    #expect(model.selectedBufferID == notJoined.id)
    model.nextBuffer()
    #expect(model.selectedBufferID == query.id)
  }

  @Test func freshSnapshotResetsPaginationState() {
    let model = AppModel(transport: FixtureTransport(), defaults: isolatedDefaults())
    let bufferID = UUID()
    model.historyExhausted.insert(bufferID)
    model.historyLoading.insert(bufferID)

    model.applySnapshot(
      StateSnapshot(networks: [], buffers: [], initialMessages: [:], members: [:]))

    #expect(model.historyExhausted.isEmpty)
    #expect(model.historyLoading.isEmpty)
  }

  private func isolatedDefaults() -> UserDefaults {
    UserDefaults(suiteName: "xyz.endymion.lurker.tests.\(UUID().uuidString)")!
  }

  private func network(id: UUID) -> Network {
    Network(
      id: id,
      name: "Libera",
      kind: "irc",
      host: "irc.libera.chat",
      port: 6697,
      tls: true,
      nick: "shrike",
      sortOrder: 0
    )
  }

  private func buffer(
    _ name: String,
    kind: String = "channel",
    networkID: UUID,
    joined: Bool = true,
    pinned: Bool = false
  ) -> Buffer {
    Buffer(
      id: UUID(),
      networkID: networkID,
      name: name,
      kind: kind,
      joined: joined,
      showEmbeds: true,
      showPresenceEvents: true,
      collapsePresenceEvents: false,
      pinned: pinned,
      unread: 0,
      mentions: 0
    )
  }
}
