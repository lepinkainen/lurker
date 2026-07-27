#if os(macOS)
  import AppKit
#endif
import Foundation
import UserNotifications

@MainActor
final class NotificationManager: NSObject, UNUserNotificationCenterDelegate {
  static let shared = NotificationManager()
  nonisolated static let bufferIDKey = "bufferID"

  private var openBuffer: (@MainActor @Sendable (UUID) -> Void)?
  private var configured = false

  func configure(openBuffer: @escaping @MainActor @Sendable (UUID) -> Void) {
    self.openBuffer = openBuffer
    guard !configured else { return }
    configured = true
    UNUserNotificationCenter.current().delegate = self
    Task {
      _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [
        .alert, .sound,
      ])
    }
  }

  func post(message: Message, buffer: Buffer, network: Network?) {
    let content = UNMutableNotificationContent()
    content.title = buffer.kind == "query" ? message.sender : "\(message.sender) in \(buffer.name)"
    content.subtitle = network?.name ?? ""
    content.body = message.content
    content.sound = .default
    content.userInfo = [Self.bufferIDKey: buffer.id.uuidString]
    let request = UNNotificationRequest(
      identifier: message.id.uuidString,
      content: content,
      trigger: nil
    )
    UNUserNotificationCenter.current().add(request)
  }

  /// Reflect the unread-mention total on the app icon. macOS uses the dock tile
  /// badge; iOS uses the notification-center badge count.
  func setBadge(_ count: Int) {
    guard !ProcessInfo.isPreviewOrUITest else { return }
    #if os(macOS)
      NSApp.dockTile.badgeLabel = count > 0 ? String(count) : nil
    #else
      UNUserNotificationCenter.current().setBadgeCount(count)
    #endif
  }

  nonisolated func userNotificationCenter(
    _: UNUserNotificationCenter,
    willPresent _: UNNotification
  ) async -> UNNotificationPresentationOptions {
    []
  }

  nonisolated func userNotificationCenter(
    _: UNUserNotificationCenter,
    didReceive response: UNNotificationResponse
  ) async {
    guard let raw = response.notification.request.content.userInfo[Self.bufferIDKey] as? String,
      let id = UUID(uuidString: raw)
    else {
      return
    }
    await MainActor.run {
      #if os(macOS)
        NSApp.activate()
      #endif
      openBuffer?(id)
    }
  }
}
