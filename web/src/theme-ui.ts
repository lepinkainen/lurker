import { applyTheme, loadInitialTheme, persistThemeId, type Theme } from "./theme";

export function applyThemeDefaults() {
  document.documentElement.dataset.density = "dense";
}

export async function initThemeSelector() {
  const sel = document.getElementById("theme-select") as HTMLSelectElement | null;
  const { themes, active } = await loadInitialTheme();
  if (!sel) return;
  if (themes.length === 0) {
    sel.hidden = true;
    return;
  }
  sel.innerHTML = "";
  for (const theme of themes) {
    const opt = document.createElement("option");
    opt.value = theme.id;
    opt.textContent = theme.name;
    sel.appendChild(opt);
  }
  if (active) sel.value = active.id;
  sel.addEventListener("change", () => {
    const pick = themes.find((theme: Theme) => theme.id === sel.value);
    if (!pick) return;
    applyTheme(pick);
    persistThemeId(pick.id);
  });
}
