import { readdir, readFile } from "node:fs/promises";
import { extname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";

const root = fileURLToPath(new URL("../../", import.meta.url));
// Mermaid's parser sanitizes labels using DOMPurify, which needs a DOM.
// jsdom supplies that in Node; no scripts, network requests, or rendering run.
const dom = new JSDOM("");
globalThis.window = dom.window;
globalThis.document = dom.window.document;
const { default: mermaid } = await import("mermaid");
mermaid.initialize({ startOnLoad: false });
const { version } = JSON.parse(await readFile(new URL("../node_modules/mermaid/package.json", import.meta.url), "utf8"));

async function documentation(dir, recursive) {
  const files = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory() && recursive) files.push(...await documentation(path, true));
    else if (entry.isFile() && [".md", ".html", ".mmd"].includes(extname(path))) files.push(path);
  }
  return files;
}

function diagrams(text, path) {
  const lineAt = (index) => text.slice(0, index).split("\n").length;
  if (extname(path) === ".mmd") return [{ source: text, line: 1 }];
  if (extname(path) === ".html") {
    const page = new JSDOM(text, { includeNodeLocations: true });
    try {
      return [...page.window.document.querySelectorAll('script[type="text/plain"][id^="source-"], pre.mermaid')]
        .map((node) => ({ source: node.textContent, line: page.nodeLocation(node).startLine }));
    } finally {
      page.window.close();
    }
  }
  // Match all fenced blocks so examples containing nested fences are skipped.
  const blocks = text.matchAll(/^ {0,3}(`{3,}|~{3,})([^\n]*)\n([\s\S]*?)^ {0,3}\1[ \t]*\r?$/gm);
  return [...blocks]
    .filter((block) => block[2].trim() === "mermaid")
    .map((block) => ({ source: block[3], line: lineAt(block.index) + 1 }));
}

const files = process.argv.length > 2
  ? process.argv.slice(2)
  : [...await documentation(root, false), ...await documentation(join(root, "ai-docs"), true)];
let checked = 0;
let failed = false;
for (const path of files.sort()) {
  const text = await readFile(path, "utf8");
  const label = relative(root, path);
  for (const match of text.matchAll(/https:\/\/cdn\.jsdelivr\.net\/npm\/mermaid@([^/]+)\/dist\//g)) {
    if (match[1] !== version) {
      console.error(`${label}: Mermaid CDN version ${match[1]} differs from lint version ${version}`);
      failed = true;
    }
  }
  for (const { source, line } of diagrams(text, path)) {
    try {
      await mermaid.parse(source);
      checked++;
    } catch (error) {
      console.error(`${label}:${line}: ${error.message}`);
      failed = true;
    }
  }
}
dom.window.close();
if (checked === 0 && !failed) {
  console.error("No Mermaid diagrams found.");
  failed = true;
}
if (!failed) console.log(`Mermaid syntax OK: ${checked} diagrams (Mermaid ${version})`);
process.exitCode = failed ? 1 : 0;
