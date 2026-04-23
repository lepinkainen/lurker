export type Theme = {
  id: string;
  name: string;
  colors: Record<string, string>;
  fonts: { sans?: string; mono?: string };
  nicks: { saturation?: number; lightness?: number; hue_offset?: number };
};

const STORAGE_KEY = "lurker.theme.id";

export async function fetchThemes(): Promise<Theme[]> {
  const res = await fetch("/api/themes", { headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`themes: HTTP ${res.status}`);
  const data = (await res.json()) as { themes?: Theme[] };
  return data.themes ?? [];
}

export function applyTheme(t: Theme): void {
  const root = document.documentElement;
  for (const [k, v] of Object.entries(t.colors ?? {})) {
    root.style.setProperty(`--${k}`, v);
  }
  if (t.fonts?.sans) root.style.setProperty("--sans", t.fonts.sans);
  if (t.fonts?.mono) root.style.setProperty("--mono", t.fonts.mono);
  const sat = t.nicks?.saturation;
  const light = t.nicks?.lightness;
  const hueOff = t.nicks?.hue_offset;
  if (typeof sat === "number") root.style.setProperty("--nick-sat", `${sat}%`);
  if (typeof light === "number") root.style.setProperty("--nick-light", `${light}%`);
  if (typeof hueOff === "number") root.style.setProperty("--nick-hue-offset", `${hueOff}deg`);
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
