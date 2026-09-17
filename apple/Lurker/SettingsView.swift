import SwiftUI

// MARK: - SettingsView

struct SettingsView: View {

  // MARK: Internal

  var body: some View {
    Form {
      Section("Connection") {
        TextField("Lurker server", text: $server, prompt: Text("http://localhost:8080"))
          .textFieldStyle(.roundedBorder)
          .autocorrectionDisabled()
          #if os(iOS)
          .textInputAutocapitalization(.never)
          .keyboardType(.URL)
          #endif
        Text("Only localhost, Tailnet 100.64.0.0/10 addresses, and MagicDNS names are accepted.")
          .font(.footnote)
          .foregroundStyle(.secondary)
        HStack {
          connectionLabel
          Spacer()
          Button("Verify and Save") {
            save()
          }
          .disabled(server.isEmpty || saving)
        }
        if let error {
          Text(error)
            .font(.body)
            .foregroundStyle(.red)
        }
      }

      Section("Notifications") {
        Toggle(
          "Notify for mentions while Lurker is in the background",
          isOn: Binding(
            get: { model.notificationsEnabled },
            set: { value in model.setNotificationsEnabled(value) },
          ),
        )
      }

      Section("About") {
        LabeledContent("Application", value: "Lurker")
        LabeledContent("Bundle identifier", value: "xyz.endymion.lurker")
        if let identity = model.serviceIdentity {
          LabeledContent("Server", value: "\(identity.version) (\(identity.hash))")
          LabeledContent("Server built", value: identity.buildTimeLabel())
        }
      }
    }
    .formStyle(.grouped)
    .padding()
    .onAppear {
      server = model.configuredURL?.absoluteString ?? ""
    }
  }

  // MARK: Private

  @Environment(AppModel.self) private var model
  @State private var server = ""
  @State private var saving = false
  @State private var error: String?

  @ViewBuilder
  private var connectionLabel: some View {
    if saving {
      ProgressView()
        .controlSize(.small)
    } else {
      Label(model.connectionState.label, systemImage: model.connectionState.symbol)
        .font(.footnote)
        .foregroundStyle(.secondary)
    }
  }

  private func save() {
    saving = true
    error = nil
    Task {
      do {
        try await model.saveServer(server)
        server = model.configuredURL?.absoluteString ?? server
      } catch {
        self.error = error.localizedDescription
      }
      saving = false
    }
  }

}

// MARK: - ConnectionEditor

struct ConnectionEditor: View {

  // MARK: Internal

  let required: Bool

  var body: some View {
    VStack(alignment: .leading, spacing: 18) {
      HStack(alignment: .top, spacing: 14) {
        Image(systemName: "bubble.left.and.text.bubble.right")
          .font(.system(size: 34, weight: .medium))
          .foregroundStyle(.mint)
          .symbolRenderingMode(.hierarchical)
        VStack(alignment: .leading, spacing: 5) {
          Text(required ? "Connect to Lurker" : "Change Lurker Server")
            .font(.title.weight(.semibold))
          Text("Enter the private HTTP address of your bouncer.")
            .foregroundStyle(.secondary)
        }
      }

      TextField("Server address", text: $server, prompt: Text("http://localhost:8080"))
        .textFieldStyle(.roundedBorder)
        .font(.title3.monospaced())
        .autocorrectionDisabled()
        #if os(iOS)
        .textInputAutocapitalization(.never)
        .keyboardType(.URL)
        #endif
        .onSubmit(connect)

      VStack(alignment: .leading, spacing: 5) {
        Label("localhost and loopback addresses", systemImage: "desktopcomputer")
        Label(
          "Tailscale 100.64.0.0/10 addresses",
          systemImage: "point.3.connected.trianglepath.dotted",
        )
        Label("MagicDNS names and *.ts.net hosts", systemImage: "network")
      }
      .font(.footnote)
      .foregroundStyle(.secondary)

      if let error {
        Label(error, systemImage: "exclamationmark.triangle.fill")
          .font(.body)
          .foregroundStyle(.red)
      }

      HStack {
        if !required {
          Button("Cancel", role: .cancel) {
            dismiss()
          }
        }
        Spacer()
        Button("Connect") {
          connect()
        }
        .keyboardShortcut(.defaultAction)
        .disabled(server.isEmpty || saving)
      }
    }
    .padding(24)
    #if os(macOS)
    .frame(width: 480)
    #endif
    .interactiveDismissDisabled(required)
    .onAppear {
      server = model.configuredURL?.absoluteString ?? "http://localhost:8080"
    }
  }

  // MARK: Private

  @Environment(AppModel.self) private var model
  @Environment(\.dismiss) private var dismiss
  @State private var server = ""
  @State private var saving = false
  @State private var error: String?

  private func connect() {
    guard !server.isEmpty, !saving else { return }
    saving = true
    error = nil
    Task {
      do {
        try await model.saveServer(server)
        dismiss()
      } catch {
        self.error = error.localizedDescription
      }
      saving = false
    }
  }

}

#if DEBUG
#Preview {
  SettingsView()
    .environment(AppModel.preview())
    .tint(.mint)
    #if os(macOS)
    .frame(width: 500, height: 330)
    #endif
}
#endif
