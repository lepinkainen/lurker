import { loadInitialTheme } from "./theme";

export function applyThemeDefaults() {
  document.documentElement.dataset.density = "dense";
}

// Fetches the theme list and applies the active theme as a side effect. The
// theme picker itself lives in the settings view (see settings-dialog.ts);
// this just ensures the correct theme is applied on initial page load.
export async function initTheme(): Promise<void> {
  await loadInitialTheme();
}
