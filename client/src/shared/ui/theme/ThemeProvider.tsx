import { createContext } from 'react';
import type { ReactNode } from 'react';

import { darkTheme } from '@shared/ui/theme/darkTheme';
import type { ColorScheme, Theme } from '@shared/ui/theme/theme';
import { useThemePreference } from '@shared/ui/theme/themePreference';
import { themes } from '@shared/ui/theme/themes';

export const ThemeContext = createContext<Theme>(darkTheme);

export function ThemeProvider({ scheme, children }: { scheme?: ColorScheme; children: ReactNode }) {
  const preferred = useThemePreference((s) => s.scheme);
  return (
    <ThemeContext.Provider value={themes[scheme ?? preferred]}>{children}</ThemeContext.Provider>
  );
}
