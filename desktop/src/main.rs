// Prevents an extra console window on Windows in release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

use tauri::{DragDropEvent, Manager, Url, WebviewUrl, WebviewWindowBuilder, WindowEvent};

const DEFAULT_BACKEND_URL: &str = "http://localhost:8080";

/// Path of the sentinel URL the init script navigates to when a link should
/// leave the app. `on_navigation` intercepts it before it ever loads.
const SENTINEL_PATH: &str = "/__open_external";

/// Sentinel the paste init script navigates to when the page is handed a paste
/// it cannot turn into a `File` itself. Same trick as `SENTINEL_PATH`: init
/// scripts can't call Rust directly.
const PASTE_SENTINEL_PATH: &str = "/__paste_upload";

/// Sentinels the drop gate navigates to once the page has decided whether it
/// can accept a dropped file right now. Tauri's native drag-drop handler fires
/// for the whole window and Rust cannot see the page's state, so the page is
/// asked before anything is uploaded.
const DROP_ACCEPT_PATH: &str = "/__drop_accept";
const DROP_REJECT_PATH: &str = "/__drop_reject";

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

/// Injected alongside the external-link script. WebKitGTK never gives the page
/// a `File` for a pasted image (issue #149): a file copied in a file manager
/// arrives as a `text/uri-list` string, and a bare image arrives as an entirely
/// empty `clipboardData`. Either way `web/src/input-upload.ts`'s
/// `DataTransfer.files` / `items[].kind === "file"` path is unreachable, so this
/// hands the paste to Rust instead -- passing the URI along when there is one,
/// and otherwise letting Rust read the system clipboard.
///
/// It deliberately stands aside whenever the page *can* cope: a paste carrying
/// text, or one with a real file attached, is left entirely alone.
const PASTE_INIT_SCRIPT: &str = r##"
(function () {
  document.addEventListener("paste", function (ev) {
    var cd = ev.clipboardData;
    if (!cd) return;

    // A payload carrying text is a text paste; same rule as input-upload.ts.
    try {
      if (cd.getData("text/plain").trim()) return;
    } catch (e) {
      return;
    }

    // If the page can see a real file, its own handler already works.
    if (cd.files && cd.files.length) return;
    if (cd.items) {
      for (var i = 0; i < cd.items.length; i++) {
        if (cd.items[i].kind === "file") return;
      }
    }

    // Only act for the composer, mirroring isForeignEditable's intent.
    var input = document.getElementById("input");
    if (!input || input.disabled) return;
    var t = ev.target;
    if (t && t !== input && t.nodeType === 1) {
      if (t.isContentEditable || t.tagName === "INPUT" || t.tagName === "TEXTAREA") return;
    }

    var uri = "";
    var types = cd.types ? Array.prototype.slice.call(cd.types) : [];
    if (types.indexOf("text/uri-list") >= 0) {
      var lines = (cd.getData("text/uri-list") || "").split(/\r?\n/);
      for (var j = 0; j < lines.length; j++) {
        var line = lines[j].trim();
        // A uri-list comment line starts with "#".
        if (line && line.charAt(0) !== "#") {
          uri = line;
          break;
        }
      }
    }

    ev.preventDefault();
    window.location.href =
      window.location.origin + "/__paste_upload" + (uri ? "?uri=" + encodeURIComponent(uri) : "");
  }, true);
})();
"##;

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

/// Evaluated in the page after every native drop. Rust holds the dropped path
/// but cannot see whether the composer is usable, so the page answers with a
/// sentinel navigation.
///
/// The test is deliberately about *state*, not the drop's coordinates: a drop
/// anywhere in the window is accepted, matching how other chat clients behave
/// and keeping the target something larger than the composer strip. What must
/// not happen is uploading into a composer the user cannot reach: behind an
/// open `<dialog>`, or under the settings view, which covers the message pane
/// while leaving the composer in the DOM.
///
/// Note this does *not* refuse a drop when the composer is disabled, even
/// though `PASTE_INIT_SCRIPT` does refuse a paste then. That asymmetry is
/// inherited, not invented: the frontend's own paste handler bails on
/// `inputEl.disabled` (`web/src/input-upload.ts`), while its upload button
/// never does -- the paperclip uploads fine on a channel you have not joined.
/// A drop is the same gesture as that button, so it follows the button.
const DROP_GATE_SCRIPT: &str = r##"
(function () {
  function go(path, why) {
    window.location.href =
      window.location.origin + path + (why ? "?why=" + encodeURIComponent(why) : "");
  }
  // Real <dialog>s (channel switcher, shortcuts help, network form) plus the
  // settings view, which is a modeless div[role=dialog] rather than a <dialog>
  // and so is not matched by dialog[open].
  if (document.querySelector('dialog[open], [role="dialog"]')) {
    go("/__drop_reject", "a dialog is open");
    return;
  }
  if (!document.getElementById("input")) {
    go("/__drop_reject", "the composer is not available");
    return;
  }
  go("/__drop_accept");
})();
"##;

/// The page's answer to a dropped file.
#[derive(Debug, PartialEq, Eq)]
enum DropGate {
    Accept,
    Reject(String),
}

/// Recognise the drop gate's reply, or `None` for any other navigation.
fn drop_gate(url: &Url) -> Option<DropGate> {
    match url.path() {
        DROP_ACCEPT_PATH => Some(DropGate::Accept),
        DROP_REJECT_PATH => Some(DropGate::Reject(
            url.query_pairs()
                .find(|(key, _)| key == "why")
                .map(|(_, value)| value.into_owned())
                .unwrap_or_else(|| "the composer is not ready".to_string()),
        )),
        _ => None,
    }
}

/// Recognise the paste sentinel. Returns the `uri` query param when the page
/// found one, or `None` inside `Some` when Rust should read the clipboard.
fn paste_sentinel(url: &Url) -> Option<Option<String>> {
    if url.path() != PASTE_SENTINEL_PATH {
        return None;
    }
    Some(
        url.query_pairs()
            .find(|(key, _)| key == "uri")
            .map(|(_, value)| value.into_owned()),
    )
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

/// Mirrors `PRECHECK_MIN_BYTES` in `web/src/input-upload.ts`: below this size,
/// hashing the file and making a precheck round-trip costs more than simply
/// uploading it.
const PRECHECK_MIN_BYTES: usize = 1 << 20;

/// Join `path` onto the backend URL. `Url::join` replaces the last path
/// segment, so a `backend_url` written without a trailing slash would
/// otherwise swallow its own final segment.
fn endpoint(backend: &Url, path: &str) -> Option<Url> {
    let mut base = backend.clone();
    if !base.path().ends_with('/') {
        let with_slash = format!("{}/", base.path());
        base.set_path(&with_slash);
    }
    base.join(path).ok()
}

/// Escape `raw` for embedding in a double-quoted JavaScript string literal.
/// Filenames and backend error text both reach `eval` this way, and neither is
/// under our control.
fn js_string(raw: &str) -> String {
    let mut out = String::with_capacity(raw.len() + 2);
    out.push('"');
    for c in raw.chars() {
        match c {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '<' => out.push_str("\\u003c"),
            _ => out.push(c),
        }
    }
    out.push('"');
    out
}

/// Lowercase hex of a SHA-256 digest, matching `hex.EncodeToString` on the
/// backend (`media/browse.go`), which rejects uppercase. Written out by hand
/// because sha2 0.11 returns hybrid-array's `Array`, which has no `LowerHex`.
fn sha256_hex(bytes: &[u8]) -> String {
    <sha2::Sha256 as sha2::Digest>::digest(bytes)
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

/// Ask the backend whether it already stores this content, mirroring
/// `checkExisting`. Any failure means "upload normally", never an error.
async fn existing_url(client: &reqwest::Client, backend: &Url, bytes: &[u8]) -> Option<String> {
    if bytes.len() < PRECHECK_MIN_BYTES {
        return None;
    }
    let hash = sha256_hex(bytes);
    let url = endpoint(backend, &format!("api/media/exists?hash={hash}"))?;
    let res = client.get(url).send().await.ok()?;
    if !res.status().is_success() {
        return None;
    }
    let body = res.text().await.ok()?;
    let parsed: serde_json::Value = serde_json::from_str(&body).ok()?;
    parsed.get("url")?.as_str().map(str::to_owned)
}

/// Upload `bytes` under `filename` and return the stored public URL. The
/// Rust-side twin of `uploadFile` in `web/src/input-upload.ts`, including its
/// dedupe precheck. Drops and pastes both land here.
async fn upload_bytes(
    client: &reqwest::Client,
    backend: &Url,
    bytes: Vec<u8>,
    filename: String,
) -> Result<String, String> {
    if let Some(url) = existing_url(client, backend, &bytes).await {
        return Ok(url);
    }
    let form = reqwest::multipart::Form::new().part(
        "file",
        reqwest::multipart::Part::bytes(bytes).file_name(filename),
    );
    let url = endpoint(backend, "api/upload").ok_or("invalid backend URL")?;
    let res = client
        .post(url)
        .multipart(form)
        .send()
        .await
        .map_err(|e| e.to_string())?;
    let status = res.status();
    let body = res.text().await.unwrap_or_default();
    if !status.is_success() {
        let detail = body.trim();
        return Err(if detail.is_empty() {
            format!("upload failed ({})", status.as_u16())
        } else {
            detail.to_string()
        });
    }
    let parsed: serde_json::Value =
        serde_json::from_str(&body).map_err(|_| "upload response was not JSON".to_string())?;
    parsed
        .get("url")
        .and_then(|u| u.as_str())
        .map(str::to_owned)
        .ok_or_else(|| "upload response missing url".to_string())
}

/// Mirrors `MIME_EXT` / `uploadFilename`: a clipboard image arrives with no
/// name, but the backend requires a filename on the multipart part.
fn pasted_filename(mime: &str) -> String {
    let ext = match mime.split(';').next().unwrap_or("").trim() {
        "image/png" => "png",
        "image/jpeg" => "jpg",
        "image/gif" => "gif",
        "image/webp" => "webp",
        _ => "bin",
    };
    let millis = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis())
        .unwrap_or(0);
    format!("pasted-{millis}.{ext}")
}

/// A `file://` URI as a local path, or `None` for anything else. Paste
/// payloads carry remote URLs too, and those are not ours to upload.
fn file_path_from_uri(uri: &str) -> Option<PathBuf> {
    let parsed = Url::parse(uri.trim()).ok()?;
    if parsed.scheme() != "file" {
        return None;
    }
    parsed.to_file_path().ok()
}

/// MIME types the system clipboard is currently offering.
///
/// WebKitGTK shows the page almost none of this (issue #149): a bare image
/// reaches the DOM as an empty `clipboardData`, and a file copied in a file
/// manager as an unreadable string. The shell therefore asks the compositor
/// directly rather than trusting what the page was handed.
#[cfg(target_os = "linux")]
fn clipboard_mime_types() -> Result<Vec<String>, String> {
    use wl_clipboard_rs::paste::{get_mime_types, ClipboardType, Seat};
    let offered = get_mime_types(ClipboardType::Regular, Seat::Unspecified)
        .map_err(|e| format!("clipboard unavailable: {e}"))?;
    let mut types: Vec<String> = offered.into_iter().collect();
    types.sort();
    Ok(types)
}

/// Read one clipboard flavour verbatim, so a pasted JPEG stays a JPEG.
#[cfg(target_os = "linux")]
fn clipboard_read(mime: &str) -> Result<(Vec<u8>, String), String> {
    use std::io::Read;
    use wl_clipboard_rs::paste::{get_contents, ClipboardType, MimeType, Seat};
    let (mut reader, actual) = get_contents(
        ClipboardType::Regular,
        Seat::Unspecified,
        MimeType::Specific(mime),
    )
    .map_err(|e| format!("clipboard read failed: {e}"))?;
    let mut bytes = Vec::new();
    reader
        .read_to_end(&mut bytes)
        .map_err(|e| format!("clipboard read failed: {e}"))?;
    Ok((bytes, actual))
}

#[cfg(not(target_os = "linux"))]
fn clipboard_mime_types() -> Result<Vec<String>, String> {
    Err("clipboard paste is only wired up on Linux".to_string())
}

#[cfg(not(target_os = "linux"))]
fn clipboard_read(_mime: &str) -> Result<(Vec<u8>, String), String> {
    Err("clipboard paste is only wired up on Linux".to_string())
}

/// Prefer PNG when several image flavours are offered: it is what screenshot
/// tools put up, and choosing deterministically keeps pastes consistent.
fn pick_image_mime(offered: &[String]) -> Option<String> {
    if offered.iter().any(|m| m == "image/png") {
        return Some("image/png".to_string());
    }
    offered.iter().find(|m| m.starts_with("image/")).cloned()
}

/// Upload `bytes` and insert the resulting URL at the composer caret, driving
/// the composer's own note element for progress and failure.
async fn upload_and_insert(
    window: &tauri::WebviewWindow,
    backend: &Url,
    bytes: Vec<u8>,
    filename: String,
) {
    let _ = window.eval(note_script(
        Some(&format!("Uploading {filename}\u{2026}")),
        false,
        true,
    ));
    let client = reqwest::Client::new();
    match upload_bytes(&client, backend, bytes, filename).await {
        Ok(url) => {
            let _ = window.eval(insert_script(&url));
            let _ = window.eval(note_script(None, false, false));
        }
        Err(err) => {
            let _ = window.eval(note_script(
                Some(&format!("Upload failed: {err}")),
                true,
                false,
            ));
        }
    }
}

/// Read a local file and upload it. Used by both a drop and a paste of a file
/// reference, which are the same operation wearing different clothes.
async fn upload_path(window: &tauri::WebviewWindow, backend: &Url, path: &Path) {
    let filename = path
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| "image".to_string());
    match std::fs::read(path) {
        Ok(bytes) => upload_and_insert(window, backend, bytes, filename).await,
        Err(err) => {
            let _ = window.eval(note_script(
                Some(&format!("Upload failed: {}: {err}", path.display())),
                true,
                false,
            ));
        }
    }
}

/// Handle the paste sentinel.
///
/// `uri` is whatever the page managed to read from `text/uri-list`, which is
/// only sometimes populated; everything else is recovered from the system
/// clipboard here. Order matters: a file reference the page already resolved is
/// cheapest, then image data, then a file reference read from the clipboard
/// ourselves for when WebKitGTK would not hand the page its own `getData`.
async fn handle_paste(window: &tauri::WebviewWindow, backend: &Url, uri: Option<String>) {
    if let Some(path) = uri.as_deref().and_then(file_path_from_uri) {
        upload_path(window, backend, &path).await;
        return;
    }

    let offered = match clipboard_mime_types() {
        Ok(types) => types,
        Err(err) => {
            let _ = window.eval(note_script(
                Some(&format!("Paste failed: {err}")),
                true,
                false,
            ));
            return;
        }
    };

    if let Some(mime) = pick_image_mime(&offered) {
        match clipboard_read(&mime) {
            Ok((bytes, actual)) => {
                upload_and_insert(window, backend, bytes, pasted_filename(&actual)).await;
            }
            Err(err) => {
                let _ = window.eval(note_script(
                    Some(&format!("Paste failed: {err}")),
                    true,
                    false,
                ));
            }
        }
        return;
    }

    if offered.iter().any(|m| m == "text/uri-list") {
        if let Ok((bytes, _)) = clipboard_read("text/uri-list") {
            let text = String::from_utf8_lossy(&bytes);
            let found = text
                .lines()
                .map(str::trim)
                // A uri-list comment line starts with '#'.
                .filter(|line| !line.is_empty() && !line.starts_with('#'))
                .find_map(file_path_from_uri);
            if let Some(path) = found {
                upload_path(window, backend, &path).await;
                return;
            }
        }
    }

    let _ = window.eval(note_script(
        Some(&format!(
            "Paste failed: clipboard has no image or local file (offered: {})",
            if offered.is_empty() {
                "nothing".to_string()
            } else {
                offered.join(", ")
            }
        )),
        true,
        false,
    ));
}

/// Show, or clear, the composer's upload note.
///
/// The note normally lives inside the composer as `.upload-note`, matching what
/// the web UI does for the same events. But that element is `position:absolute`
/// inside `.inputbar` at `z-index: 11`, so it cannot be lifted above a dialog or
/// the settings view -- raising its z-index is useless from inside a lower
/// stacking context. When something is covering the composer the note is therefore
/// parented to `<body>` and pinned to the viewport instead. Same element id either
/// way, so a note never appears twice.
///
/// Errors clear themselves after the same 8s the frontend's `showNote` uses;
/// progress notes stay until the caller clears them.
fn note_script(text: Option<&str>, error: bool, uploading: bool) -> String {
    let body = match text {
        Some(t) => format!(
            r#"  var covered = !!document.querySelector('dialog[open], [role="dialog"]');
  var host = covered ? document.body : form;
  if (note && note.parentNode !== host) {{
    note.parentNode.removeChild(note);
    note = null;
  }}
  if (!note) {{
    note = document.createElement("div");
    note.id = NOTE_ID;
    host.appendChild(note);
  }}
  note.className = {};
  note.hidden = false;
  note.textContent = {};
  note.style.cssText = covered
    ? "position:fixed;left:50%;right:auto;bottom:24px;transform:translateX(-50%);" +
      "max-width:min(90vw,32rem);margin:0;z-index:2147483647;"
    : "";
  if (note.dismissTimer) {{
    clearTimeout(note.dismissTimer);
    note.dismissTimer = null;
  }}
  if ({}) {{
    note.dismissTimer = setTimeout(function () {{
      if (note.parentNode) {{
        note.parentNode.removeChild(note);
      }}
    }}, 8000);
  }}"#,
            js_string(if error {
                "upload-note err"
            } else {
                "upload-note"
            }),
            js_string(t),
            error
        ),
        None => r#"  if (note && note.parentNode) {
    note.parentNode.removeChild(note);
  }"#
        .to_string(),
    };
    format!(
        r#"(function () {{
  var NOTE_ID = "lurker-upload-note";
  var el = document.getElementById("input");
  var form = el && el.closest ? el.closest("form") : null;
  if (!form) return;
  var note = document.getElementById(NOTE_ID);
{body}
  form.classList.toggle("uploading", {uploading});
}})();"#
    )
}

/// Insert the uploaded URL at the composer caret, mirroring
/// `insertTextAtCursor`, then fire a real `input` event so the command popup
/// and anything else bound in `web/src/input.ts` stays in step.
fn insert_script(url: &str) -> String {
    format!(
        r#"(function () {{
  var el = document.getElementById("input");
  if (!el) return;
  var text = {};
  var start = el.selectionStart != null ? el.selectionStart : el.value.length;
  var end = el.selectionEnd != null ? el.selectionEnd : start;
  var before = el.value.slice(0, start);
  var after = el.value.slice(end);
  var prefix = before && !/\s$/u.test(before) ? " " : "";
  var suffix = after && !/^\s/u.test(after) ? " " : "";
  var inserted = prefix + text + suffix;
  el.value = before + inserted + after;
  var caret = before.length + inserted.length;
  el.setSelectionRange(caret, caret);
  el.dispatchEvent(new Event("input", {{ bubbles: true }}));
  el.focus();
}})();"#,
        js_string(url)
    )
}

fn main() {
    let backend_url = resolve_backend_url();
    let url: Url = backend_url
        .parse()
        .unwrap_or_else(|_| panic!("invalid backend URL: {backend_url}"));
    let backend = url.clone();

    tauri::Builder::default()
        .setup(move |app| {
            // on_navigation runs before the window exists, so reach it later
            // through the app handle rather than capturing it.
            let handle = app.handle().clone();
            let nav_backend = backend.clone();
            // Holds the path from the most recent native drop until the page
            // says whether it can accept it. Single-use: taken on either reply,
            // so a stale path can never be uploaded by a later navigation.
            let pending_drop: Arc<Mutex<Option<PathBuf>>> = Arc::new(Mutex::new(None));
            let nav_pending = Arc::clone(&pending_drop);
            WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
                .title("Lurker")
                .inner_size(1200.0, 800.0)
                .resizable(true)
                // Tauri's native drag-drop handler stays ON, and the Rust side
                // below does the upload. On Linux/WebKitGTK the page can never
                // do it: a dropped file arrives as `text/uri-list` with no
                // entry in `DataTransfer.files`, so the frontend's HTML5 drop
                // path (web/src/input-upload.ts) is unreachable, and with the
                // native handler disabled the webview's own default navigates
                // the window to `file:///...` and replaces the chat UI
                // outright. Letting Tauri consume the drop fixes both. macOS
                // could still use the page's own handler, but it has a native
                // client of its own and one code path is worth more here.
                .initialization_script(EXTERNAL_LINK_INIT_SCRIPT)
                .initialization_script(PASTE_INIT_SCRIPT)
                .on_navigation(move |url| {
                    if let Some(gate) = drop_gate(url) {
                        let path = nav_pending.lock().ok().and_then(|mut slot| slot.take());
                        let handle = handle.clone();
                        let backend = nav_backend.clone();
                        tauri::async_runtime::spawn(async move {
                            let Some(window) = handle.get_webview_window("main") else {
                                return;
                            };
                            match (gate, path) {
                                (DropGate::Accept, Some(path)) => {
                                    upload_path(&window, &backend, &path).await;
                                }
                                (DropGate::Reject(why), _) => {
                                    let _ = window.eval(note_script(
                                        Some(&format!("Can't upload here — {why}")),
                                        true,
                                        false,
                                    ));
                                }
                                (DropGate::Accept, None) => {}
                            }
                        });
                        return false;
                    }
                    if let Some(uri) = paste_sentinel(url) {
                        let handle = handle.clone();
                        let backend = nav_backend.clone();
                        tauri::async_runtime::spawn(async move {
                            if let Some(window) = handle.get_webview_window("main") {
                                handle_paste(&window, &backend, uri).await;
                            }
                        });
                        return false;
                    }
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

            let window = app.get_webview_window("main").expect("main window exists");
            let upload_window = window.clone();
            let drop_pending = Arc::clone(&pending_drop);
            window.on_window_event(move |event| {
                let WindowEvent::DragDrop(DragDropEvent::Drop { paths, .. }) = event else {
                    return;
                };
                // The composer holds one URL at a time and the frontend's own
                // drop path took `files[0]`; keep that shape.
                let Some(path) = paths.first().cloned() else {
                    return;
                };
                // Park the path and let the page decide; see DROP_GATE_SCRIPT.
                if let Ok(mut slot) = drop_pending.lock() {
                    *slot = Some(path);
                }
                let _ = upload_window.eval(DROP_GATE_SCRIPT);
            });
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running the Lurker desktop shell");
}

#[cfg(test)]
mod tests {
    use super::{
        drop_gate, endpoint, file_path_from_uri, is_openable_scheme, js_string, parse_backend_url,
        paste_sentinel, pasted_filename, pick_image_mime, sentinel_target, sha256_hex, DropGate,
    };
    use tauri::Url;

    #[test]
    fn sha256_hex_matches_the_backend_encoding() {
        // Reference vector: SHA-256("abc"). Lowercase and unseparated, because
        // media/browse.go compares against hex.EncodeToString and 400s on
        // uppercase.
        assert_eq!(
            sha256_hex(b"abc"),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
    }

    #[test]
    fn endpoint_appends_to_origin_without_trailing_slash() {
        let backend = Url::parse("http://localhost:8080").unwrap();
        assert_eq!(
            endpoint(&backend, "api/upload").unwrap().as_str(),
            "http://localhost:8080/api/upload"
        );
    }

    #[test]
    fn endpoint_does_not_swallow_a_base_path_segment() {
        // Url::join would turn ".../lurker" + "api/upload" into ".../api/upload".
        let backend = Url::parse("http://box.tailnet.ts.net/lurker").unwrap();
        assert_eq!(
            endpoint(&backend, "api/upload").unwrap().as_str(),
            "http://box.tailnet.ts.net/lurker/api/upload"
        );
    }

    #[test]
    fn js_string_escapes_quotes_backslashes_and_newlines() {
        assert_eq!(js_string(r#"a"b\c"#), r#""a\"b\\c""#);
        assert_eq!(js_string("a\nb"), r#""a\nb""#);
    }

    #[test]
    fn paste_sentinel_reports_uri_when_present() {
        let url = Url::parse(
            "http://localhost:8080/__paste_upload?uri=file%3A%2F%2F%2Fhome%2Fme%2Fa%20b.png",
        )
        .unwrap();
        assert_eq!(
            paste_sentinel(&url),
            Some(Some("file:///home/me/a b.png".to_string()))
        );
    }

    #[test]
    fn paste_sentinel_reports_no_uri_for_a_bare_clipboard_paste() {
        let url = Url::parse("http://localhost:8080/__paste_upload").unwrap();
        assert_eq!(paste_sentinel(&url), Some(None));
    }

    #[test]
    fn paste_sentinel_ignores_other_paths() {
        let url = Url::parse("http://localhost:8080/api/state").unwrap();
        assert_eq!(paste_sentinel(&url), None);
    }

    #[test]
    fn drop_gate_recognises_acceptance() {
        let url = Url::parse("http://localhost:8080/__drop_accept").unwrap();
        assert_eq!(drop_gate(&url), Some(DropGate::Accept));
    }

    #[test]
    fn drop_gate_carries_the_rejection_reason() {
        let url =
            Url::parse("http://localhost:8080/__drop_reject?why=a%20dialog%20is%20open").unwrap();
        assert_eq!(
            drop_gate(&url),
            Some(DropGate::Reject("a dialog is open".to_string()))
        );
    }

    #[test]
    fn drop_gate_rejection_without_a_reason_still_explains_itself() {
        let url = Url::parse("http://localhost:8080/__drop_reject").unwrap();
        assert_eq!(
            drop_gate(&url),
            Some(DropGate::Reject("the composer is not ready".to_string()))
        );
    }

    #[test]
    fn drop_gate_ignores_ordinary_navigation() {
        let url = Url::parse("http://localhost:8080/api/state").unwrap();
        assert_eq!(drop_gate(&url), None);
    }

    #[test]
    fn file_path_from_uri_accepts_file_urls_and_decodes_them() {
        assert_eq!(
            file_path_from_uri("file:///home/me/a%20b.png"),
            Some(std::path::PathBuf::from("/home/me/a b.png"))
        );
    }

    #[test]
    fn file_path_from_uri_rejects_remote_and_malformed_uris() {
        // A uri-list can carry a remote URL; that is not ours to upload.
        assert_eq!(file_path_from_uri("https://example.com/a.png"), None);
        assert_eq!(file_path_from_uri("not a uri"), None);
    }

    #[test]
    fn pick_image_mime_prefers_png_over_other_image_flavours() {
        let offered = vec![
            "image/bmp".to_string(),
            "image/png".to_string(),
            "text/html".to_string(),
        ];
        assert_eq!(pick_image_mime(&offered), Some("image/png".to_string()));
    }

    #[test]
    fn pick_image_mime_falls_back_and_ignores_non_images() {
        assert_eq!(
            pick_image_mime(&["image/tiff".to_string()]),
            Some("image/tiff".to_string())
        );
        assert_eq!(pick_image_mime(&["text/uri-list".to_string()]), None);
    }

    #[test]
    fn pasted_filename_maps_known_image_types() {
        assert!(pasted_filename("image/png").ends_with(".png"));
        assert!(pasted_filename("image/jpeg").ends_with(".jpg"));
        // Wayland offers parameters on some MIME strings.
        assert!(pasted_filename("image/webp; charset=binary").ends_with(".webp"));
        assert!(pasted_filename("application/octet-stream").ends_with(".bin"));
    }

    #[test]
    fn js_string_escapes_angle_brackets() {
        // Filenames reach eval() verbatim; keep them from closing a script tag.
        assert_eq!(js_string("</script>"), r#""\u003c/script>""#);
    }

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
        let url =
            Url::parse("http://localhost:8080/some/other/path?url=http://evil.example").unwrap();
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
