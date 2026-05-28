import { createContext } from 'react';
import type { ReactNode } from 'react';

import { darkTheme } from './darkTheme';
import type { ColorScheme, Theme } from './theme';
import { themes } from './themes';

// AIDEV-NOTE: ADR-0008 — the context default IS darkTheme, so components that
// read useTheme() with no provider mounted (e.g. the bare-rendered auth screens
// in jest) resolve to dark instead of throwing. v1 always supplies dark; flip
// `scheme` to a useColorScheme() read to enable the light theme later — no
// component change required.
export const ThemeContext = createContext<Theme>(darkTheme);

export function ThemeProvider({
  scheme = 'dark',
  children,
}: {
  scheme?: ColorScheme;
  children: ReactNode;
}) {
  return <ThemeContext.Provider value={themes[scheme]}>{children}</ThemeContext.Provider>;
}
