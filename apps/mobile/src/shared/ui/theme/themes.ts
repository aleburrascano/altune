import { darkTheme } from './darkTheme';
import { lightTheme } from './lightTheme';
import type { ColorScheme, Theme } from './theme';

export const themes: Record<ColorScheme, Theme> = {
  dark: darkTheme,
  light: lightTheme,
};
