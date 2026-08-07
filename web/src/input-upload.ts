import { updateCmdPop } from "./input-command-popup";

const TRAILING_WHITESPACE_RE = /\s$/u;
const LEADING_WHITESPACE_RE = /^\s/u;

export type InputUploadDeps = {
  inputEl: HTMLInputElement;
  inputForm: HTMLFormElement;
  uploadInputEl: HTMLInputElement;
  uploadButtonEl: HTMLButtonElement;
  cmdPopEl: HTMLElement;
};

export function insertTextAtCursor(inputEl: HTMLInputElement, text: string) {
  const start = inputEl.selectionStart ?? inputEl.value.length;
  const end = inputEl.selectionEnd ?? start;
  const before = inputEl.value.slice(0, start);
  const after = inputEl.value.slice(end);
  const prefix = before && !TRAILING_WHITESPACE_RE.test(before) ? " " : "";
  const suffix = after && !LEADING_WHITESPACE_RE.test(after) ? " " : "";
  const inserted = `${prefix}${text}${suffix}`;
  inputEl.value = before + inserted + after;
  const caret = before.length + inserted.length;
  inputEl.setSelectionRange(caret, caret);
}

// Below this size, hashing the file and making a precheck round-trip costs
// more than just uploading it, so we skip straight to the upload.
const PRECHECK_MIN_BYTES = 1 << 20; // 1 MiB

async function sha256Hex(buf: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", buf);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// checkExisting asks the server whether these exact bytes are already stored,
// so a duplicate upload can be skipped entirely. Best-effort: it returns null
// (→ upload normally) on any obstacle. crypto.subtle only exists in secure
// contexts (https / localhost); over a plain-http tailnet origin it's
// undefined, so the precheck is simply skipped there and the server's own
// upload-time dedup still applies.
async function checkExisting(file: File): Promise<string | null> {
  if (file.size < PRECHECK_MIN_BYTES || !globalThis.crypto?.subtle) return null;
  try {
    const hash = await sha256Hex(await file.arrayBuffer());
    const res = await fetch(`/api/media/exists?hash=${hash}`);
    if (!res.ok) return null; // 404 miss, or any error → upload normally
    const data = (await res.json()) as { url?: string };
    return data.url ?? null;
  } catch {
    return null;
  }
}

// Extensions for the image types the backend stores, used to name clipboard
// files that arrive without one.
const MIME_EXT: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
};

// uploadFilename picks the filename sent on the multipart part. The server
// rejects an empty one, and pasted files routinely have no name — Safari (iOS
// especially) hands back a File with an empty `name` for a pasted photo — so
// synthesize one from the MIME type in that case.
export function uploadFilename(file: File): string {
  if (file.name) return file.name;
  const ext = MIME_EXT[file.type.toLowerCase()] ?? "bin";
  return `pasted-${Date.now()}.${ext}`;
}

export async function uploadFile(file: File): Promise<string> {
  const existing = await checkExisting(file);
  if (existing) return existing;

  const form = new FormData();
  form.set("file", file, uploadFilename(file));
  const res = await fetch("/api/upload", { method: "POST", body: form });
  if (!res.ok) {
    const detail = (await res.text()).trim();
    throw new Error(detail || `upload failed (${res.status})`);
  }
  const data = (await res.json()) as { url?: string };
  if (!data.url) throw new Error("upload response missing url");
  return data.url;
}

// An upload is the one composer action with no immediate visible result: the
// URL only appears once the round-trip finishes. Without a note the whole
// thing looks like nothing happened — worst on a phone, where the console is
// out of reach and the upload is slow enough to doubt.
const NOTE_CLASS = "upload-note";
const NOTE_ERROR_MS = 8000;

let noteTimer: ReturnType<typeof setTimeout> | undefined;

function showNote(form: HTMLFormElement, text: string, kind: "info" | "error") {
  clearTimeout(noteTimer);
  let el = form.querySelector<HTMLElement>(`.${NOTE_CLASS}`);
  if (!el) {
    el = document.createElement("div");
    el.className = NOTE_CLASS;
    el.setAttribute("role", "status");
    form.prepend(el);
  }
  el.classList.toggle("err", kind === "error");
  el.textContent = text;
  el.hidden = false;
  if (kind === "error") noteTimer = setTimeout(() => clearNote(form), NOTE_ERROR_MS);
}

function clearNote(form: HTMLFormElement) {
  clearTimeout(noteTimer);
  const el = form.querySelector<HTMLElement>(`.${NOTE_CLASS}`);
  if (el) el.hidden = true;
}

const MAX_NOTE_DETAIL = 120;

function errorText(err: unknown): string {
  const detail = err instanceof Error ? err.message : String(err);
  const trimmed = detail.trim();
  if (!trimmed) return "Upload failed";
  const short = trimmed.length > MAX_NOTE_DETAIL ? `${trimmed.slice(0, MAX_NOTE_DETAIL)}…` : trimmed;
  return `Upload failed: ${short}`;
}

async function uploadAndInsert(file: File, deps: InputUploadDeps) {
  deps.uploadButtonEl.disabled = true;
  deps.inputForm.classList.add("uploading");
  showNote(deps.inputForm, `Uploading ${file.name || "image"}…`, "info");
  try {
    const url = await uploadFile(file);
    insertTextAtCursor(deps.inputEl, url);
    deps.inputEl.focus();
    updateCmdPop(deps.inputEl, deps.cmdPopEl);
    clearNote(deps.inputForm);
  } catch (err) {
    console.error("upload failed", err);
    showNote(deps.inputForm, errorText(err), "error");
  } finally {
    deps.uploadButtonEl.disabled = false;
    deps.inputForm.classList.remove("uploading");
  }
}

// clipboardImage picks the first image out of a paste payload. `files` is the
// straightforward path; Safari can leave it empty while still exposing the
// image through `items`, so fall back to that.
export function clipboardImage(data: DataTransfer | null): File | null {
  if (!data) return null;
  for (const file of Array.from(data.files ?? [])) {
    if (file.type.startsWith("image/")) return file;
  }
  for (const item of Array.from(data.items ?? [])) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (file?.type.startsWith("image/")) return file;
  }
  return null;
}

// A paste aimed at some other text field (network form, search box, dialogs)
// belongs to that field, not to the composer.
function isForeignEditable(target: EventTarget | null, inputEl: HTMLInputElement): boolean {
  if (target === inputEl || !(target instanceof HTMLElement)) return false;
  return target.isContentEditable || target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement;
}

export function bindUploadHandlers(deps: InputUploadDeps) {
  // Paste is bound on the document, not on the composer input: iOS Safari does
  // not reliably deliver an image paste to a plain <input type=text>, and the
  // photo is often pasted with the keyboard callout while focus sits elsewhere.
  // A document listener catches it wherever it lands and still sees composer
  // pastes, which bubble — so exactly one listener, no double upload.
  document.addEventListener("paste", (ev) => {
    if (deps.inputEl.disabled || isForeignEditable(ev.target, deps.inputEl)) return;
    // A payload that also carries text is a text paste: rich text drags images
    // along, and copying an image off a web page attaches its source URL. Let
    // the browser paste that; only a text-free payload is an image paste.
    if (ev.clipboardData?.getData("text/plain").trim()) return;
    const file = clipboardImage(ev.clipboardData);
    if (!file) return;
    ev.preventDefault();
    uploadAndInsert(file, deps).catch((err: unknown) => console.error("upload failed", err));
  });
  deps.uploadButtonEl.addEventListener("click", () => deps.uploadInputEl.click());
  deps.uploadInputEl.addEventListener("change", () => {
    const file = deps.uploadInputEl.files?.[0];
    deps.uploadInputEl.value = "";
    if (file) uploadAndInsert(file, deps).catch((err: unknown) => console.error("upload failed", err));
  });
  deps.inputForm.addEventListener("dragover", (ev) => {
    if (!ev.dataTransfer?.files || ev.dataTransfer.files.length === 0) return;
    ev.preventDefault();
    deps.inputForm.classList.add("upload-dragover");
  });
  deps.inputForm.addEventListener("dragleave", () => deps.inputForm.classList.remove("upload-dragover"));
  deps.inputForm.addEventListener("drop", (ev) => {
    deps.inputForm.classList.remove("upload-dragover");
    const file = ev.dataTransfer?.files?.[0];
    if (!file) return;
    ev.preventDefault();
    uploadAndInsert(file, deps).catch((err: unknown) => console.error("upload failed", err));
  });
}
