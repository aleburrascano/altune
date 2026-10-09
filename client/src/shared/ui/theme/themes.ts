import { darkTheme } from '@shared/ui/theme/darkTheme';
import { lightTheme } from '@shared/ui/theme/lightTheme';
import type { ColorScheme, Theme } from '@shared/ui/theme/theme';

export const themes: Record<ColorScheme, Theme> = {
  dark: darkTheme,
  light: lightTheme,
};
