import SwiftUI

// MARK: - ImageCache

/// Shared image cache + loader for preview/thumbnail images in the timeline.
///
/// SwiftUI's built-in `AsyncImage` ties its fetch to the view's lifetime: when
/// a row scrolls off (or the `ScrollView` reflows during auto-scroll), the
/// view is torn down, its `.task` is cancelled, and the in-flight download is
/// thrown away. In a `LazyVStack` timeline that reappears/reflows constantly,
/// previews end up re-fetching from scratch every time and often never finish
/// loading. `ImageCache` fixes this by owning the fetch itself: the
/// `Task` that performs the download belongs to the cache, not to any one
/// view, so it keeps running (and populates the cache) even if every
/// subscribed view disappears mid-fetch. Later views for the same URL just
/// read the cached result.
@MainActor
final class ImageCache {

  // MARK: Lifecycle

  private init() { }

  // MARK: Internal

  static let shared = ImageCache()

  /// Synchronous cache read, for views that want to render immediately
  /// without a loading flash when the image is already available.
  func cached(for url: URL) -> PlatformImage? {
    cache.object(forKey: url as NSURL)
  }

  /// Returns the image for `url`, fetching it if necessary. Concurrent
  /// callers for the same URL share one in-flight fetch. The fetch task is
  /// owned by the cache (not the caller), so cancelling the caller's await
  /// (e.g. a SwiftUI `.task` whose view disappeared) does not cancel the
  /// underlying download — it completes and populates the cache regardless,
  /// ready for the next view that asks.
  func image(for url: URL) async -> PlatformImage? {
    if let cached = cached(for: url) {
      return cached
    }
    if let existing = inFlight[url] {
      return await existing.value
    }
    let task = Task<PlatformImage?, Never> { [weak self] in
      let result = try? await URLSession.shared.data(from: url)
      let image = result.flatMap { data, _ in PlatformImage(data: data) }
      if let image {
        self?.cache.setObject(image, forKey: url as NSURL)
      }
      // Failures are not cached: a reappearing row (scroll back into view)
      // gets to retry rather than being stuck on a transient network error.
      self?.inFlight[url] = nil
      return image
    }
    inFlight[url] = task
    return await task.value
  }

  // MARK: Private

  private let cache = NSCache<NSURL, PlatformImage>()
  private var inFlight = [URL: Task<PlatformImage?, Never>]()

}

// MARK: - CachedAsyncImage

/// Cached replacement for `AsyncImage`, backed by `ImageCache`. Fetching is
/// driven by `.task(id:)` so a URL change restarts loading, but — unlike
/// `AsyncImage` — a cancelled await (row scrolled away) does not cancel the
/// underlying fetch; it just stops this particular view from observing the
/// result, and the cache is populated for whoever asks next.
struct CachedAsyncImage<Content: View, Placeholder: View, Failure: View>: View {

  // MARK: Lifecycle

  init(
    url: URL,
    @ViewBuilder content: @escaping (Image) -> Content,
    @ViewBuilder placeholder: @escaping () -> Placeholder,
    @ViewBuilder failure: @escaping () -> Failure,
  ) {
    self.url = url
    self.content = content
    self.placeholder = placeholder
    self.failure = failure
  }

  // MARK: Internal

  let url: URL
  let content: (Image) -> Content
  let placeholder: () -> Placeholder
  let failure: () -> Failure

  var body: some View {
    Group {
      if let image {
        content(Image(platformImage: image))
      } else if failed {
        failure()
      } else {
        placeholder()
      }
    }
    // Show a cached hit synchronously, before the `.task` even runs, so
    // scrolling a row back into view never re-flashes the placeholder.
    .onAppear {
      if image == nil {
        image = ImageCache.shared.cached(for: url)
      }
    }
    .task(id: url) {
      if let cached = ImageCache.shared.cached(for: url) {
        image = cached
        failed = false
        return
      }
      failed = false
      let result = await ImageCache.shared.image(for: url)
      guard !Task.isCancelled else { return }
      if let result {
        image = result
      } else {
        failed = true
      }
    }
  }

  // MARK: Private

  @State private var image: PlatformImage?
  @State private var failed = false

}

extension CachedAsyncImage where Failure == Placeholder {
  /// Convenience initializer matching `AsyncImage(url:content:placeholder:)`:
  /// with no distinct failure view supplied, the placeholder is shown on
  /// failure too (matches the two-closure `AsyncImage` initializer, which
  /// uses the same view for both `.empty` and `.failure` phases).
  init(
    url: URL,
    @ViewBuilder content: @escaping (Image) -> Content,
    @ViewBuilder placeholder: @escaping () -> Placeholder,
  ) {
    self.url = url
    self.content = content
    self.placeholder = placeholder
    failure = placeholder
  }
}
