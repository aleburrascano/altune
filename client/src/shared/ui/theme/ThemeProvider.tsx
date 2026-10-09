import { createContext } from 'react';
import type { ReactNode } from 'react';

import { darkTheme, themes, type ColorScheme, type Theme } from '@shared/ui/theme/theme';
import { useThemePreference } from '@shared/ui/theme/themePreference';

export const ThemeContext = createContext<Theme>(darkTheme);

export function ThemeProvider({ scheme, children }: { scheme?: ColorScheme; children: ReactNode }) {
  const preferred = useThemePreference((s) => s.scheme);
  return (
    <ThemeContext.Provider value={themes[scheme ?? preferred]}>{children}</ThemeContext.Provider>
  );
}
