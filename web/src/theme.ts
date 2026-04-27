export type Theme = {
  id: string;
  name: string;
  colors: Record<string, string>;
  fonts: { sans?: string; mono?: string };
  nicks: { saturation?: number; lightness?: number; hue_offset?: number; oklch_l?: number; oklch_c?: number };
  color_scheme?: "dark" | "light";
};

const STORAGE_KEY = "lurker.theme.id";

export async function fetchThemes(): Promise<Theme[]> {
  const res = await fetch("/api/themes", { headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`themes: HTTP ${res.status}`);
  const data = (await res.json()) as { themes?: Theme[] };
  return data.themes ?? [];
}

const THEME_VARS = [
  "bg-0",
  "bg-1",
  "bg-2",
  "bg-3",
  "bg-4",
  "bg-5",
  "bg-input",
  "fg-0",
  "fg-1",
  "fg-2",
  "fg-3",
  "hair",
  "hair-strong",
  "accent",
  "accent-soft",
  "accent-strong",
  "green",
  "yellow",
  "orange",
  "red",
  "magenta",
  "cyan",
  "mention",
  "mention-soft",
  "mention-soft-hover",
  "surface-contrast-fg",
  "topicbar-rule",
  "sans",
  "mono",
  "nick-l",
  "nick-c",
];

export function applyTheme(t: Theme): void {
  const root = document.documentElement;
  for (const v of THEME_VARS) root.style.removeProperty(`--${v}`);
  for (const [k, v] of Object.entries(t.colors ?? {})) {
    root.style.setProperty(`--${k}`, v);
  }
  if (t.fonts?.sans) root.style.setProperty("--sans", t.fonts.sans);
  if (t.fonts?.mono) root.style.setProperty("--mono", t.fonts.mono);
  const nickL = t.nicks?.oklch_l;
  const nickC = t.nicks?.oklch_c;
  if (typeof nickL === "number") root.style.setProperty("--nick-l", `${nickL}%`);
  if (typeof nickC === "number") root.style.setProperty("--nick-c", `${nickC}`);
  root.style.setProperty("color-scheme", t.color_scheme ?? "dark");
  root.dataset.theme = t.id;
}

export function storedThemeId(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function persistThemeId(id: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, id);
  } catch {
    /* ignore */
  }
}

export async function loadInitialTheme(): Promise<{ themes: Theme[]; active: Theme | null }> {
  let themes: Theme[] = [];
  try {
    themes = await fetchThemes();
  } catch (e) {
    console.warn("theme fetch failed", e);
    return { themes: [], active: null };
  }
  if (themes.length === 0) return { themes, active: null };
  const wantedId = storedThemeId();
  const fallback = themes.find((t) => t.id === "tokyo-night") ?? themes[0];
  const active = themes.find((t) => t.id === wantedId) ?? fallback;
  applyTheme(active);
  return { themes, active };
}
