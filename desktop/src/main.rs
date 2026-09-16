// Prevents an extra console window on Windows in release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::path::PathBuf;

use tauri::{Url, WebviewUrl, WebviewWindowBuilder};

const DEFAULT_BACKEND_URL: &str = "http://localhost:8080";

/// Path of the sentinel URL the init script navigates to when a link should
/// leave the app. `on_navigation` intercepts it before it ever loads.
const SENTINEL_PATH: &str = "/__open_external";

/// Injected before the page's own scripts run. WKWebView/WebKitGTK have no
/// default handler for `target="_blank"` (or cross-origin) link clicks, so
/// without this the frontend's `<a target="_blank">` links (see
/// `web/src/format.ts`, `web/src/preview.ts`) are just dropped. This
/// listener intercepts those clicks and hands the resolved URL to
/// `on_navigation` below via a same-origin sentinel URL, since init scripts
/// can't call Rust/opener APIs directly.
const EXTERNAL_LINK_INIT_SCRIPT: &str = r#"
(function () {
  document.addEventListener("click", function (event) {
    var link = event.target && event.target.closest ? event.target.closest("a[href]") : null;
    if (!link) return;

    var resolved = link.href;
    var isBlank = link.target === "_blank";
    var isCrossOrigin;
    try {
      isCrossOrigin = new URL(resolved, window.location.href).origin !== window.location.origin;
    } catch (e) {
      isCrossOrigin = true;
    }
    if (!isBlank && !isCrossOrigin) return;

    event.preventDefault();
    window.location.href =
      window.location.origin + "/__open_external?url=" + encodeURIComponent(resolved);
  }, true);
})();
"#;

/// Resolve the Lurker backend URL the desktop shell should point its window at.
///
/// Order: `LURKER_URL` env var, then `backend_url:` in
/// `~/.config/lurker/desktop.yaml`, then the localhost default. Mirrors
/// `cmd/tui/config.go`'s `backend_url` convention for the TUI client.
fn resolve_backend_url() -> String {
    if let Ok(url) = std::env::var("LURKER_URL") {
        if !url.trim().is_empty() {
            return url;
        }
    }

    if let Some(config_dir) = dirs_config_path() {
        if let Ok(contents) = std::fs::read_to_string(config_dir) {
            if let Some(url) = parse_backend_url(&contents) {
                return url;
            }
        }
    }

    DEFAULT_BACKEND_URL.to_string()
}

fn dirs_config_path() -> Option<PathBuf> {
    let home = std::env::var_os("HOME")?;
    Some(PathBuf::from(home).join(".config/lurker/desktop.yaml"))
}

// ponytail: line-scan instead of real YAML for a single `backend_url:` key;
// swap to serde_yaml if desktop.yaml ever grows past one key.
fn parse_backend_url(contents: &str) -> Option<String> {
    for line in contents.lines() {
        let line = line.trim();
        if let Some(rest) = line.strip_prefix("backend_url:") {
            let value = rest.trim().trim_matches(|c| c == '"' || c == '\'');
            if !value.is_empty() {
                return Some(value.to_string());
            }
        }
    }
    None
}

/// If `url` is the sentinel navigation the init script emits, extract and
/// percent-decode its `url` query param. Returns `None` for any other URL.
fn sentinel_target(url: &Url) -> Option<String> {
    if url.path() != SENTINEL_PATH {
        return None;
    }
    url.query_pairs()
        .find(|(key, _)| key == "url")
        .map(|(_, value)| value.into_owned())
}

/// Trust boundary: `url` originates from page content (including remote IRC
/// message text), so only hand it to the system opener if it's plain
/// http(s). Rejects `file:`, `javascript:`, and any other scheme.
fn is_openable_scheme(url: &Url) -> bool {
    matches!(url.scheme(), "http" | "https")
}

/// Opens `url` in the system's default browser without blocking the UI
/// thread (spawn, not wait).
fn open_in_browser(url: &str) {
    #[cfg(target_os = "macos")]
    {
        let _ = std::process::Command::new("open").arg(url).spawn();
    }
    #[cfg(target_os = "linux")]
    {
        let _ = std::process::Command::new("xdg-open").arg(url).spawn();
    }
    #[cfg(not(any(target_os = "macos", target_os = "linux")))]
    {
        // ponytail: no opener wired for other targets; desktop shell only
        // ships for macOS/Linux today. Add a Windows branch (`cmd /C start`)
        // if that target ever ships.
        let _ = url;
    }
}

fn main() {
    let backend_url = resolve_backend_url();
    let url = backend_url
        .parse()
        .unwrap_or_else(|_| panic!("invalid backend URL: {backend_url}"));

    tauri::Builder::default()
        .setup(move |app| {
            WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
                .title("Lurker")
                .inner_size(1200.0, 800.0)
                .resizable(true)
                // The native drag-drop handler intercepts dropped files before
                // the page sees them; the frontend has its own HTML5
                // dragover/drop handling for uploads (web/src/input-upload.ts),
                // so the native one must stay off or drag-to-upload silently
                // swallows the file.
                .disable_drag_drop_handler()
                .initialization_script(EXTERNAL_LINK_INIT_SCRIPT)
                .on_navigation(|url| {
                    let Some(target) = sentinel_target(url) else {
                        return true;
                    };
                    if let Ok(target_url) = Url::parse(&target) {
                        if is_openable_scheme(&target_url) {
                            open_in_browser(target_url.as_str());
                        }
                    }
                    false
                })
                .build()?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running the Lurker desktop shell");
}

#[cfg(test)]
mod tests {
    use super::{is_openable_scheme, parse_backend_url, sentinel_target};
    use tauri::Url;

    #[test]
    fn sentinel_target_extracts_and_decodes_url_param() {
        let url = Url::parse(
            "http://localhost:8080/__open_external?url=http%3A%2F%2Fexample.com%2Fpath%3Fa%3Db",
        )
        .unwrap();
        assert_eq!(
            sentinel_target(&url),
            Some("http://example.com/path?a=b".to_string())
        );
    }

    #[test]
    fn sentinel_target_ignores_non_sentinel_paths() {
        let url = Url::parse("http://localhost:8080/some/other/path?url=http://evil.example").unwrap();
        assert_eq!(sentinel_target(&url), None);
    }

    #[test]
    fn sentinel_target_none_without_url_param() {
        let url = Url::parse("http://localhost:8080/__open_external").unwrap();
        assert_eq!(sentinel_target(&url), None);
    }

    #[test]
    fn scheme_guard_accepts_http_and_https() {
        assert!(is_openable_scheme(
            &Url::parse("http://example.com").unwrap()
        ));
        assert!(is_openable_scheme(
            &Url::parse("https://example.com").unwrap()
        ));
    }

    #[test]
    fn scheme_guard_rejects_dangerous_schemes() {
        assert!(!is_openable_scheme(
            &Url::parse("javascript:alert(1)").unwrap()
        ));
        assert!(!is_openable_scheme(
            &Url::parse("file:///etc/passwd").unwrap()
        ));
        assert!(!is_openable_scheme(
            &Url::parse("myapp://open?x=1").unwrap()
        ));
    }

    #[test]
    fn parses_plain_value() {
        assert_eq!(
            parse_backend_url("backend_url: http://example.com:9000\n"),
            Some("http://example.com:9000".to_string())
        );
    }

    #[test]
    fn parses_quoted_value() {
        assert_eq!(
            parse_backend_url("backend_url: \"http://example.com\"\n"),
            Some("http://example.com".to_string())
        );
    }

    #[test]
    fn ignores_unrelated_keys_and_missing_file() {
        assert_eq!(parse_backend_url("other_key: foo\n"), None);
        assert_eq!(parse_backend_url(""), None);
    }
}
