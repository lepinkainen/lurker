import SwiftUI

struct MembersInspector: View {
  @Environment(AppModel.self) private var model
  @State private var filter = ""

  var body: some View {
    VStack(spacing: 0) {
      HStack {
        Text("Members")
          .font(.title3)
        Spacer()
        Text(filtered.count.formatted())
          .font(.footnote.monospacedDigit())
          .foregroundStyle(.secondary)
      }
      .padding(12)

      TextField("Filter members", text: $filter)
        .textFieldStyle(.roundedBorder)
        .padding(.horizontal, 10)
        .padding(.bottom, 8)

      List(filtered) { member in
        MemberRow(member: member, networkID: model.selectedBuffer?.networkID)
          .listRowSeparator(.hidden)
          .contextMenu {
            Text(member.nick)
            if let realname = member.realname, !realname.isEmpty {
              Text(realname)
            }
            Divider()
            Button("Message \(member.nick)") {
              sendTarget("query", member.nick)
            }
            Button("WHOIS \(member.nick)") {
              sendTarget("whois", member.nick)
            }
            Divider()
            if member.prefix == "@" {
              Button("Deop \(member.nick)") {
                sendChannelModeTarget("deop", member.nick)
              }
            } else {
              Button("Op \(member.nick)") {
                sendChannelModeTarget("op", member.nick)
              }
            }
            if member.prefix == "+" {
              Button("Devoice \(member.nick)") {
                sendChannelModeTarget("devoice", member.nick)
              }
            } else {
              Button("Voice \(member.nick)") {
                sendChannelModeTarget("voice", member.nick)
              }
            }
            Button("Kick \(member.nick)") {
              sendChannelModeTarget("kick", member.nick)
            }
            Divider()
            Button("Mute \(member.nick)") {
              muteTarget(member.nick, muted: true)
            }
            Button("Unmute \(member.nick)") {
              muteTarget(member.nick, muted: false)
            }
            Divider()
            Button("Copy Nickname") {
              Clipboard.copy(member.nick)
            }
          }
      }
      .listStyle(.inset)
      .overlay {
        if model.selectedBuffer?.kind != "channel" {
          ContentUnavailableView("No Members", systemImage: "person.2.slash")
        } else if filtered.isEmpty {
          ContentUnavailableView.search(text: filter)
        }
      }
    }
  }

  private var filtered: [Member] {
    guard !filter.isEmpty else { return model.selectedMembers }
    return model.selectedMembers.filter {
      $0.nick.localizedCaseInsensitiveContains(filter)
        || $0.realname?.localizedCaseInsensitiveContains(filter) == true
    }
  }

  private func sendTarget(_ type: String, _ nick: String) {
    guard let networkID = model.selectedBuffer?.networkID else { return }
    model.command(ClientCommand(type: type, networkID: networkID, target: nick))
  }

  private func sendChannelModeTarget(_ type: String, _ nick: String) {
    guard let bufferID = model.selectedBuffer?.id else { return }
    model.command(ClientCommand(type: type, bufferID: bufferID, target: nick))
  }

  private func muteTarget(_ nick: String, muted: Bool) {
    guard let networkID = model.selectedBuffer?.networkID else { return }
    if muted {
      model.mute(nick: nick, in: networkID)
    } else {
      model.unmute(nick: nick, in: networkID)
    }
  }
}

private struct MemberRow: View {
  let member: Member
  let networkID: UUID?

  var body: some View {
    HStack(spacing: 7) {
      Text(member.prefix ?? " ")
        .font(.footnote.monospaced().weight(.bold))
        .foregroundStyle(prefixColor)
        .frame(width: 10)
      NickAvatar(
        nick: member.nick, colorIndex: member.color, isBot: member.bot == true,
        networkID: networkID, hasAvatar: member.hasAvatar == true)
      VStack(alignment: .leading, spacing: 1) {
        Text(member.nick)
          .font(Theme.Fonts.nick.weight(member.`self` ? .bold : .regular))
          .foregroundStyle(member.away ? .secondary : .primary)
          .lineLimit(1)
          .truncationMode(.tail)
      }
    }
    .help(member.realname ?? member.nick)
  }

  private var prefixColor: Color {
    switch member.prefix {
    case "@", "&", "~": .red
    case "%": .purple
    case "+": .green
    default: .clear
    }
  }
}
