#!/usr/bin/env node
// One-time generator for PWA / iOS home-screen icons.
// Renders web/public/favicon.svg to PNG at the sizes required by the manifest
// and iOS apple-touch-icon. Re-run after changing the source SVG.
//
//   node web/scripts/generate-icons.mjs
//
// Requires Playwright with a chromium build available locally.

import { chromium } from "playwright";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const publicDir = process.env.LURKER_PUBLIC_DIR
  ? resolve(process.env.LURKER_PUBLIC_DIR)
  : resolve(here, "..", "public");

const targets = [
  { file: "apple-touch-icon.png", size: 180, padding: 16, bg: "#0f1923" },
  { file: "icon-192.png", size: 192, padding: 0, bg: null },
  { file: "icon-512.png", size: 512, padding: 0, bg: null },
  { file: "icon-maskable-512.png", size: 512, padding: 64, bg: "#0f1923" },
];

const svg = await readFile(resolve(publicDir, "favicon.svg"), "utf8");

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ deviceScaleFactor: 1 });
  const page = await ctx.newPage();

  for (const t of targets) {
    const inner = t.size - t.padding * 2;
    const html = `<!doctype html><html><head><style>
      html,body{margin:0;padding:0;background:${t.bg ?? "transparent"};}
      .wrap{width:${t.size}px;height:${t.size}px;display:flex;align-items:center;justify-content:center;}
      .wrap > svg{width:${inner}px;height:${inner}px;display:block;}
    </style></head><body><div class="wrap">${svg}</div></body></html>`;

    await page.setViewportSize({ width: t.size, height: t.size });
    await page.setContent(html, { waitUntil: "load" });
    const buf = await page.screenshot({
      omitBackground: t.bg == null,
      clip: { x: 0, y: 0, width: t.size, height: t.size },
      type: "png",
    });
    await mkdir(publicDir, { recursive: true });
    await writeFile(resolve(publicDir, t.file), buf);
    console.log(`wrote ${t.file} (${t.size}x${t.size})`);
  }
} finally {
  await browser.close();
}
