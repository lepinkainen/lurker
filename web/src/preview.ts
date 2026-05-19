export type Preview = {
  url: string;
  kind: "image" | "opengraph" | string;
  title?: string;
  description?: string;
  image_url?: string;
  site_name?: string;
  width?: number;
  height?: number;
  mime?: string;
};

export function renderPreview(p: Preview): HTMLElement | null {
  if (p.kind === "image") return renderImagePreview(p);
  if (p.kind === "opengraph") return renderOpenGraphPreview(p);
  return null;
}

export function renderPreviews(list: Preview[] | undefined): HTMLElement | null {
  if (!list || list.length === 0) return null;
  const wrap = document.createElement("div");
  wrap.className = "previews";
  let added = 0;
  for (const p of list) {
    const el = renderPreview(p);
    if (el) {
      wrap.appendChild(el);
      added++;
    }
  }
  return added > 0 ? wrap : null;
}

function renderImagePreview(p: Preview): HTMLElement {
  const a = document.createElement("a");
  a.className = "preview preview-img";
  a.href = p.url;
  a.target = "_blank";
  a.rel = "noreferrer noopener";
  const img = document.createElement("img");
  img.src = p.url;
  img.loading = "lazy";
  img.decoding = "async";
  img.alt = "";
  if (p.width && p.height) {
    img.width = p.width;
    img.height = p.height;
  }
  a.appendChild(img);
  return a;
}

function renderOpenGraphPreview(p: Preview): HTMLElement {
  const a = document.createElement("a");
  a.className = "preview preview-og";
  a.href = p.url;
  a.target = "_blank";
  a.rel = "noreferrer noopener";

  if (p.image_url) {
    const thumb = document.createElement("img");
    thumb.className = "preview-og-thumb";
    thumb.src = p.image_url;
    thumb.loading = "lazy";
    thumb.decoding = "async";
    thumb.alt = "";
    a.appendChild(thumb);
  }

  const body = document.createElement("div");
  body.className = "preview-og-body";
  if (p.site_name) {
    const site = document.createElement("div");
    site.className = "preview-og-site";
    site.textContent = p.site_name;
    body.appendChild(site);
  }
  if (p.title) {
    const title = document.createElement("div");
    title.className = "preview-og-title";
    title.textContent = p.title;
    body.appendChild(title);
  }
  if (p.description) {
    const desc = document.createElement("div");
    desc.className = "preview-og-desc";
    desc.textContent = p.description;
    body.appendChild(desc);
  }
  a.appendChild(body);
  return a;
}
