import { useEffect, useState } from "react";
import { readLocalPreference, writeLocalPreference } from "./browser-storage";

export type Theme = "dark" | "light";
export const defaultTheme: Theme = "dark";
const storageKey = "aipermission-theme";

export function readStoredTheme(): Theme {
  const value = readLocalPreference(storageKey);
  return value === "light" || value === "dark" ? value : defaultTheme;
}

export function applyTheme(theme: Theme): void {
  if (typeof document === "undefined") return;
  document.documentElement.dataset.theme = theme;
}

export function useTheme() {
  const [theme, setTheme] = useState(readStoredTheme);

  useEffect(() => {
    applyTheme(theme);
    writeLocalPreference(storageKey, theme);
  }, [theme]);

  function toggleTheme() {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }

  return { theme, setTheme, toggleTheme };
}
