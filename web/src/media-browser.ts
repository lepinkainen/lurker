import { openDialog } from "./dialog";
import { jsonFetch } from "./http";

type MediaItem = {
  id: string;
  kind: string;
  mime: string;
  url: string;
  width: number;
  height: number;
  bytes: number;
  created_at: string;
};

type MediaListResponse = {
  items: MediaItem[];
  total: number;
  limit: number;
  offset: number;
};

const PAGE_SIZE = 60;
const SEARCH_DEBOUNCE_MS = 250;

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0 B";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = n / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = value < 10 ? 1 : 0;
  return `${value.toFixed(digits)} ${units[unit]}`;
}

async function deleteMedia(id: string): Promise<void> {
  const res = await fetch(`/api/media/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
}

function buildQuery(q: string, kind: string, offset: number): string {
  const params = new URLSearchParams();
  if (q) params.set("q", q);
  if (kind) params.set("kind", kind);
  params.set("limit", String(PAGE_SIZE));
  params.set("offset", String(offset));
  return `/api/media?${params.toString()}`;
}

function buildTile(item: MediaItem, onDeleted: () => void): HTMLElement {
  const tile = document.createElement("div");
  tile.className = "mb-tile";

  const thumb = document.createElement("img");
  thumb.className = "mb-thumb";
  thumb.src = item.url;
  thumb.loading = "lazy";
  thumb.alt = item.mime;

  const caption = document.createElement("div");
  caption.className = "mb-caption";
  caption.textContent = `${item.mime} · ${item.width}×${item.height} · ${formatBytes(item.bytes)}`;

  const delBtn = document.createElement("button");
  delBtn.type = "button";
  delBtn.className = "mb-delete";
  delBtn.textContent = "×";
  delBtn.title = "Delete";
  delBtn.addEventListener("click", () => {
    delBtn.disabled = true;
    deleteMedia(item.id)
      .then(() => {
        tile.remove();
        onDeleted();
      })
      .catch((err: unknown) => {
        delBtn.disabled = false;
        delBtn.title = err instanceof Error ? err.message : "Delete failed";
      });
  });

  tile.append(thumb, delBtn, caption);
  return tile;
}

export function openMediaBrowser(): void {
  const { dialog, close } = openDialog({ className: "mb-dialog" });

  const inner = document.createElement("div");
  inner.className = "mb-inner";

  const title = document.createElement("h2");
  title.className = "mb-title";
  title.textContent = "Media library";

  const filterRow = document.createElement("div");
  filterRow.className = "mb-filter-row";

  const searchInput = document.createElement("input");
  searchInput.type = "text";
  searchInput.className = "mb-input";
  searchInput.placeholder = "Search id or type…";

  const kindSelect = document.createElement("select");
  kindSelect.className = "mb-select";
  const kindOptions: [string, string][] = [
    ["", "All"],
    ["image", "image"],
  ];
  for (const [value, label] of kindOptions) {
    const opt = document.createElement("option");
    opt.value = value;
    opt.textContent = label;
    kindSelect.appendChild(opt);
  }

  filterRow.append(searchInput, kindSelect);

  const errEl = document.createElement("p");
  errEl.className = "mb-error";
  errEl.hidden = true;

  const emptyEl = document.createElement("p");
  emptyEl.className = "mb-empty";
  emptyEl.textContent = "No uploads yet.";
  emptyEl.hidden = true;

  const grid = document.createElement("div");
  grid.className = "mb-grid";

  const loadMoreBtn = document.createElement("button");
  loadMoreBtn.type = "button";
  loadMoreBtn.className = "mb-btn mb-btn-secondary";
  loadMoreBtn.textContent = "Load more";
  loadMoreBtn.hidden = true;

  const closeBtn = document.createElement("button");
  closeBtn.type = "button";
  closeBtn.className = "mb-btn mb-btn-ghost mb-close";
  closeBtn.textContent = "Close";
  closeBtn.addEventListener("click", close);

  let offset = 0;
  let total = 0;
  let loadedCount = 0;
  let requestSeq = 0;

  function itemCount(): number {
    return loadedCount;
  }

  async function load(reset: boolean): Promise<void> {
    if (reset) {
      offset = 0;
      loadedCount = 0;
      grid.innerHTML = "";
    }
    const seq = ++requestSeq;
    errEl.hidden = true;
    try {
      const data = await jsonFetch<MediaListResponse>(buildQuery(searchInput.value.trim(), kindSelect.value, offset));
      if (seq !== requestSeq) return; // superseded by a newer request
      total = data.total;
      for (const item of data.items) {
        grid.appendChild(
          buildTile(item, () => {
            loadedCount--;
            total--;
            emptyEl.hidden = itemCount() > 0;
            loadMoreBtn.hidden = itemCount() >= total;
          }),
        );
      }
      loadedCount += data.items.length;
      offset += data.items.length;
      emptyEl.hidden = itemCount() > 0;
      loadMoreBtn.hidden = itemCount() >= total || data.items.length === 0;
    } catch (err) {
      if (seq !== requestSeq) return;
      errEl.textContent = err instanceof Error ? err.message : "Failed to load media";
      errEl.hidden = false;
    }
  }

  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  searchInput.addEventListener("input", () => {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      load(true).catch((err: unknown) => console.error("media list", err));
    }, SEARCH_DEBOUNCE_MS);
  });
  kindSelect.addEventListener("change", () => {
    load(true).catch((err: unknown) => console.error("media list", err));
  });
  loadMoreBtn.addEventListener("click", () => {
    load(false).catch((err: unknown) => console.error("media list", err));
  });

  inner.append(title, filterRow, errEl, emptyEl, grid, loadMoreBtn, closeBtn);
  dialog.appendChild(inner);

  dialog.showModal();
  load(true).catch((err: unknown) => console.error("media list", err));
}
