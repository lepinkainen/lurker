import { afterEach, describe, expect, it, vi } from "vitest";
import {
  bindUploadHandlers,
  clipboardImage,
  type InputUploadDeps,
  uploadFile,
  uploadFilename,
} from "../src/input-upload";

function makeFile(size: number, name = "img.png"): File {
  return new File([new Uint8Array(size)], name, { type: "image/png" });
}

afterEach(() => vi.unstubAllGlobals());

describe("uploadFile client-side dedup precheck", () => {
  it("skips the precheck for small files and uploads directly", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes("/api/upload")) {
        return new Response(JSON.stringify({ url: "/uploads/abc.png" }), { status: 201 });
      }
      throw new Error(`unexpected fetch ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const url = await uploadFile(makeFile(1024)); // < 1 MiB → no precheck
    expect(url).toBe("/uploads/abc.png");
    const calls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/api/media/exists"))).toBe(false);
    expect(calls.some((u) => u.includes("/api/upload"))).toBe(true);
  });

  it("returns the existing url without uploading on a precheck hit", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes("/api/media/exists")) {
        return new Response(JSON.stringify({ url: "/uploads/dup.png" }), { status: 200 });
      }
      throw new Error(`should not upload on a dedup hit: ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const url = await uploadFile(makeFile(2 * 1024 * 1024)); // >= 1 MiB
    expect(url).toBe("/uploads/dup.png");
    const calls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/api/media/exists"))).toBe(true);
    expect(calls.some((u) => u.includes("/api/upload"))).toBe(false);
  });

  it("uploads normally when the precheck misses", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes("/api/media/exists")) return new Response("", { status: 404 });
      if (u.includes("/api/upload")) {
        return new Response(JSON.stringify({ url: "/uploads/new.png" }), { status: 201 });
      }
      throw new Error(`unexpected fetch ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const url = await uploadFile(makeFile(2 * 1024 * 1024));
    expect(url).toBe("/uploads/new.png");
    const calls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/api/media/exists"))).toBe(true);
    expect(calls.some((u) => u.includes("/api/upload"))).toBe(true);
  });
});

const PASTED_JPG_RE = /^pasted-\d+\.jpg$/u;
const PASTED_PNG_RE = /^pasted-\d+\.png$/u;

describe("uploadFilename", () => {
  it("keeps the file's own name", () => {
    expect(uploadFilename(makeFile(4, "holiday.png"))).toBe("holiday.png");
  });

  it("synthesizes a name for a nameless clipboard file", () => {
    // Safari hands back a File with an empty name for a pasted photo; the
    // server rejects an empty multipart filename.
    const file = new File([new Uint8Array(4)], "", { type: "image/jpeg" });
    expect(uploadFilename(file)).toMatch(PASTED_JPG_RE);
  });

  it("sends the synthesized name on the multipart part", async () => {
    let sentName: string | undefined;
    vi.stubGlobal("fetch", (_input: RequestInfo | URL, init?: RequestInit) => {
      const body = init?.body as FormData;
      sentName = (body.get("file") as File).name;
      return new Response(JSON.stringify({ url: "/uploads/p.png" }), { status: 201 });
    });

    await uploadFile(new File([new Uint8Array(4)], "", { type: "image/png" }));
    expect(sentName).toMatch(PASTED_PNG_RE);
  });
});

describe("clipboardImage", () => {
  it("returns the image from clipboard files", () => {
    const dt = new DataTransfer();
    dt.items.add(makeFile(8, "shot.png"));
    expect(clipboardImage(dt)?.name).toBe("shot.png");
  });

  it("falls back to items when files is empty (Safari)", () => {
    const file = makeFile(8, "from-items.png");
    const data = {
      files: [] as unknown as FileList,
      items: [{ kind: "file", getAsFile: () => file }],
    } as unknown as DataTransfer;
    expect(clipboardImage(data)?.name).toBe("from-items.png");
  });

  it("ignores non-image payloads", () => {
    const dt = new DataTransfer();
    dt.setData("text/plain", "hello");
    expect(clipboardImage(dt)).toBeNull();
    expect(clipboardImage(null)).toBeNull();
  });
});

type PasteFixture = InputUploadDeps & { form: HTMLFormElement };

const staleInputs: HTMLInputElement[] = [];

function mountComposer(): PasteFixture {
  const form = document.createElement("form");
  form.className = "inputbar";
  const inputEl = document.createElement("input");
  inputEl.type = "text";
  const uploadInputEl = document.createElement("input");
  uploadInputEl.type = "file";
  const uploadButtonEl = document.createElement("button");
  const cmdPopEl = document.createElement("div");
  form.append(inputEl, uploadInputEl, uploadButtonEl, cmdPopEl);
  document.body.append(form);
  staleInputs.push(inputEl);
  return { inputEl, inputForm: form, uploadInputEl, uploadButtonEl, cmdPopEl, form };
}

// bindUploadHandlers attaches a document-level paste listener that cannot be
// detached; disabling the composer it points at makes it a no-op for later
// tests, which is exactly the guard the handler uses in the app.
afterEach(() => {
  for (const el of staleInputs.splice(0)) el.disabled = true;
  document.body.replaceChildren();
});

function pasteImage(target: EventTarget, file = makeFile(8, "pasted.png")): ClipboardEvent {
  const dt = new DataTransfer();
  dt.items.add(file);
  const ev = new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true });
  target.dispatchEvent(ev);
  return ev;
}

describe("paste to upload", () => {
  it("uploads a pasted image and inserts its url into the composer", async () => {
    vi.stubGlobal("fetch", () => new Response(JSON.stringify({ url: "/uploads/x.png" }), { status: 201 }));
    const deps = mountComposer();
    bindUploadHandlers(deps);

    const ev = pasteImage(deps.inputEl);
    expect(ev.defaultPrevented).toBe(true);
    await vi.waitFor(() => expect(deps.inputEl.value).toBe("/uploads/x.png"));
  });

  it("catches a paste that lands outside the composer", async () => {
    // iOS Safari does not reliably deliver an image paste to a text input, so
    // the handler listens on the document and accepts pastes from elsewhere.
    vi.stubGlobal("fetch", () => new Response(JSON.stringify({ url: "/uploads/ios.png" }), { status: 201 }));
    const deps = mountComposer();
    bindUploadHandlers(deps);

    pasteImage(document.body);
    await vi.waitFor(() => expect(deps.inputEl.value).toBe("/uploads/ios.png"));
  });

  it("leaves pastes into other text fields alone", () => {
    const fetchMock = vi.fn(() => new Response(JSON.stringify({ url: "/uploads/no.png" }), { status: 201 }));
    vi.stubGlobal("fetch", fetchMock);
    const deps = mountComposer();
    bindUploadHandlers(deps);
    const other = document.createElement("input");
    document.body.append(other);

    const ev = pasteImage(other);
    expect(ev.defaultPrevented).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("leaves a paste that also carries text as a text paste", () => {
    const fetchMock = vi.fn(() => new Response("", { status: 201 }));
    vi.stubGlobal("fetch", fetchMock);
    const deps = mountComposer();
    bindUploadHandlers(deps);

    const dt = new DataTransfer();
    dt.items.add(makeFile(8, "inline.png"));
    dt.setData("text/plain", "https://example.org/inline.png");
    const ev = new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true });
    deps.inputEl.dispatchEvent(ev);

    expect(ev.defaultPrevented).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("ignores pastes while the composer is disabled", () => {
    const fetchMock = vi.fn(() => new Response("", { status: 201 }));
    vi.stubGlobal("fetch", fetchMock);
    const deps = mountComposer();
    deps.inputEl.disabled = true;
    bindUploadHandlers(deps);

    pasteImage(document.body);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows a note while uploading and reports failures", async () => {
    vi.stubGlobal("fetch", () => new Response("unsupported media type: unsupported image format", { status: 415 }));
    const deps = mountComposer();
    bindUploadHandlers(deps);

    pasteImage(deps.inputEl);
    const note = await vi.waitFor(() => {
      const el = deps.form.querySelector<HTMLElement>(".upload-note");
      expect(el?.textContent).toContain("Uploading");
      return el;
    });
    await vi.waitFor(() => {
      expect(note?.classList.contains("err")).toBe(true);
      expect(note?.textContent).toContain("unsupported image format");
    });
    expect(deps.inputEl.value).toBe("");
  });
});
