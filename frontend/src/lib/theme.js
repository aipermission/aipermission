import { useEffect, useState } from "react";

export const defaultTheme = "dark";
const storageKey = "aipermission-theme";

export function readStoredTheme() {
  if (typeof window === "undefined") return defaultTheme;
  const value = window.localStorage.getItem(storageKey);
  return value === "light" || value === "dark" ? value : defaultTheme;
}

export function applyTheme(theme) {
  if (typeof document === "undefined") return;
  document.documentElement.dataset.theme = theme;
}

export function useTheme() {
  const [theme, setTheme] = useState(readStoredTheme);

  useEffect(() => {
    applyTheme(theme);
    window.localStorage.setItem(storageKey, theme);
  }, [theme]);

  function toggleTheme() {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }

  return { theme, setTheme, toggleTheme };
}
