import SwiftUI

/// The autocomplete panel floating above the composer: nick and emoji rows
/// are selectable (arrows + Tab/Enter, or tap); the slash-command listing is
/// display-only reference, matching the web client's popups.
struct ComposerPopupView: View {
  let popup: ComposerPopup
  let selection: Int
  let accept: (Int) -> Void

  var body: some View {
    VStack(alignment: .leading, spacing: 0) {
      switch popup {
      case .nick(let nicks):
        ForEach(Array(nicks.enumerated()), id: \.element) { index, nick in
          row(isSelected: index == selection, index: index) {
            Text(nick)
              .font(.body.monospaced())
          }
        }
      case .emoji(let matches):
        ForEach(Array(matches.enumerated()), id: \.element.id) { index, match in
          row(isSelected: index == selection, index: index) {
            HStack(spacing: 8) {
              Text(match.emoji)
              Text(":\(match.name):")
                .font(.body.monospaced())
                .foregroundStyle(.secondary)
            }
          }
        }
      case .command(let commands):
        ForEach(commands) { command in
          HStack(spacing: 6) {
            Text(command.command)
              .font(.body.monospaced().weight(.semibold))
            if !command.arguments.isEmpty {
              Text(command.arguments)
                .font(.body.monospaced())
                .foregroundStyle(.secondary)
            }
            Spacer(minLength: 12)
            Text(command.description)
              .font(.callout)
              .foregroundStyle(.secondary)
          }
          .padding(.horizontal, 8)
          .padding(.vertical, 3)
        }
      case .none:
        EmptyView()
      }
    }
    .padding(4)
    .background(.regularMaterial, in: .rect(cornerRadius: 8))
    .overlay {
      RoundedRectangle(cornerRadius: 8)
        .stroke(Color.lurkerSeparator, lineWidth: 0.5)
    }
    .frame(maxWidth: 320, alignment: .leading)
  }

  @ViewBuilder private func row(
    isSelected: Bool,
    index: Int,
    @ViewBuilder content: () -> some View
  ) -> some View {
    Button {
      accept(index)
    } label: {
      HStack {
        content()
        Spacer(minLength: 0)
      }
      .padding(.horizontal, 8)
      .padding(.vertical, 3)
      .contentShape(.rect)
    }
    .buttonStyle(.plain)
    .background(
      RoundedRectangle(cornerRadius: 5)
        .fill(Color.accentColor.opacity(isSelected ? 0.22 : 0))
    )
  }
}
