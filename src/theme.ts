export const THEME_STORAGE_KEY = "pf-cinematic-theme-v3";
export function preferredTheme(): "dark" | "light" {
  try {
    return localStorage.getItem(THEME_STORAGE_KEY) === "light"
      ? "light"
      : "dark";
  } catch {
    return "dark";
  }
}
