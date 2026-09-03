import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type Theme = "system" | "light" | "dark";
type ResolvedTheme = "light" | "dark";

const STORAGE_KEY = "cc_theme";

function getStoredTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "system" || stored === "light" || stored === "dark") return stored;
  } catch {
    // localStorage can throw (private browsing, quota) — fall back to system
  }
  return "system";
}

function setStoredTheme(theme: Theme): void {
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // see getStoredTheme
  }
}

interface ThemeState {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  resolvedTheme: ResolvedTheme;
}

const ThemeContext = createContext<ThemeState | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(getStoredTheme);
  const [systemPrefersDark, setSystemPrefersDark] = useState(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  );

  useEffect(() => {
    const mql = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setSystemPrefersDark(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);

  const resolvedTheme: ResolvedTheme = theme === "system" ? (systemPrefersDark ? "dark" : "light") : theme;

  useEffect(() => {
    if (theme === "system") {
      delete document.documentElement.dataset.theme;
    } else {
      document.documentElement.dataset.theme = theme;
    }
  }, [theme]);

  const setTheme = useCallback((next: Theme) => {
    setStoredTheme(next);
    setThemeState(next);
  }, []);

  const value = useMemo(() => ({ theme, setTheme, resolvedTheme }), [theme, setTheme, resolvedTheme]);

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeState {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
