import { palette } from '@shared/ui/theme/palette';
import type { Theme } from '@shared/ui/theme/theme';

export const darkTheme: Theme = {
  scheme: 'dark',
  color: {
    canvas: palette.black,
    surface1: palette.surface1,
    surface2: palette.surface2,
    border: palette.border,
    scrim: palette.scrimDark,
    textPrimary: palette.white,
    textSecondary: palette.gray400,
    textTertiary: palette.gray500,
    accent: palette.cobalt,
    accentPressed: palette.cobaltPressed,
    accentTint: palette.cobaltTint,
    accentText: palette.cobaltText,
    onAccent: palette.pureWhite,
    warning: palette.amber,
    danger: palette.red,
    success: palette.green,
    heroGradient: [palette.cobalt, palette.cobaltSoft],
  },
};
