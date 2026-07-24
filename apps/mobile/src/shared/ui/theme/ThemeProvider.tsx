import { createContext } from 'react';
import type { ReactNode } from 'react';

import { darkTheme } from './darkTheme';
import type { ColorScheme, Theme } from './theme';
import { useThemePreference } from './themePreference';
import { themes } from './themes';

export const ThemeContext = createContext<Theme>(darkTheme);

export function ThemeProvider({ scheme, children }: { scheme?: ColorScheme; children: ReactNode }) {
  const preferred = useThemePreference((s) => s.scheme);
  return (
    <ThemeContext.Provider value={themes[scheme ?? preferred]}>{children}</ThemeContext.Provider>
  );
}
