import { createContext } from 'react';
import type { ReactNode } from 'react';

import { darkTheme } from './darkTheme';
import type { ColorScheme, Theme } from './theme';
import { useThemePreference } from './themePreference';
import { themes } from './themes';

// AIDEV-NOTE: ADR-0008 — the context default IS darkTheme, so components that
// read useTheme() with no provider mounted (e.g. the bare-rendered auth screens
// in jest) resolve to dark instead of throwing.
//
// The scheme now comes from the persisted user preference (Settings → Appearance)
// and still defaults to dark. An explicit `scheme` prop always wins, which keeps
// tests and any fixed-scheme subtree deterministic.
export const ThemeContext = createContext<Theme>(darkTheme);

export function ThemeProvider({
  scheme,
  children,
}: {
  scheme?: ColorScheme;
  children: ReactNode;
}) {
  const preferred = useThemePreference((s) => s.scheme);
  return (
    <ThemeContext.Provider value={themes[scheme ?? preferred]}>{children}</ThemeContext.Provider>
  );
}
