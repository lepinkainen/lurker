import { describe, expect, it } from "vitest";
import { type Preview, renderPreview, renderPreviews } from "../src/preview";

describe("renderPreview", () => {
  it("renders image previews with lazy loading", () => {
    const el = renderPreview({ url: "https://ex.test/p.png", kind: "image" }) as HTMLAnchorElement;
    expect(el.tagName).toBe("A");
    expect(el.className).toContain("preview-img");
    expect(el.href).toBe("https://ex.test/p.png");
    expect(el.rel).toContain("noreferrer");
    const img = el.querySelector("img") as HTMLImageElement;
    expect(img.src).toBe("https://ex.test/p.png");
    expect(img.loading).toBe("lazy");
    expect(img.decoding).toBe("async");
  });

  it("renders opengraph card with text fields", () => {
    const p: Preview = {
      url: "https://news.test/article",
      kind: "opengraph",
      title: "Headline",
      description: "Lede",
      site_name: "News",
      image_url: "https://news.test/thumb.jpg",
    };
    const el = renderPreview(p) as HTMLAnchorElement;
    expect(el.className).toContain("preview-og");
    expect(el.querySelector(".preview-og-title")?.textContent).toBe("Headline");
    expect(el.querySelector(".preview-og-desc")?.textContent).toBe("Lede");
    expect(el.querySelector(".preview-og-site")?.textContent).toBe("News");
    expect((el.querySelector("img.preview-og-thumb") as HTMLImageElement).src).toBe("https://news.test/thumb.jpg");
  });

  it("escapes untrusted OG strings via textContent (no XSS)", () => {
    const scriptEl = document.createElement("script");
    scriptEl.textContent = "alert(1)";
    const scriptTitle = scriptEl.outerHTML;
    const el = renderPreview({
      url: "https://x.test/",
      kind: "opengraph",
      title: scriptTitle,
    }) as HTMLAnchorElement;
    const title = el.querySelector(".preview-og-title") as HTMLElement;
    expect(title.textContent).toBe(scriptTitle);
    expect(title.querySelector("script")).toBeNull();
    expect(title.children.length).toBe(0);
  });

  it("returns null for unknown kinds", () => {
    expect(renderPreview({ url: "https://x", kind: "mystery" })).toBeNull();
  });
});

describe("renderPreviews", () => {
  it("returns null for empty/undefined", () => {
    expect(renderPreviews(undefined)).toBeNull();
    expect(renderPreviews([])).toBeNull();
  });

  it("skips unknown kinds and returns null when nothing usable", () => {
    expect(renderPreviews([{ url: "x", kind: "weird" }])).toBeNull();
  });

  it("wraps renderable previews in a .previews container", () => {
    const wrap = renderPreviews([
      { url: "https://a.test/pic.png", kind: "image" },
      { url: "https://b.test/", kind: "opengraph", title: "hi" },
    ]) as HTMLElement;
    expect(wrap.className).toBe("previews");
    expect(wrap.children.length).toBe(2);
  });
});
