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

export async function uploadFile(file: File): Promise<string> {
  const existing = await checkExisting(file);
  if (existing) return existing;

  const form = new FormData();
  form.set("file", file);
  const res = await fetch("/api/upload", { method: "POST", body: form });
  if (!res.ok) {
    const detail = (await res.text()).trim();
    throw new Error(detail || `upload failed (${res.status})`);
  }
  const data = (await res.json()) as { url?: string };
  if (!data.url) throw new Error("upload response missing url");
  return data.url;
}

async function uploadAndInsert(file: File, deps: InputUploadDeps) {
  deps.uploadButtonEl.disabled = true;
  deps.inputForm.classList.add("uploading");
  try {
    const url = await uploadFile(file);
    insertTextAtCursor(deps.inputEl, url);
    deps.inputEl.focus();
    updateCmdPop(deps.inputEl, deps.cmdPopEl);
  } catch (err) {
    console.error("upload failed", err);
  } finally {
    deps.uploadButtonEl.disabled = false;
    deps.inputForm.classList.remove("uploading");
  }
}

export function bindUploadHandlers(deps: InputUploadDeps) {
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
