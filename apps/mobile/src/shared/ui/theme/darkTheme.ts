import { palette } from './palette';
import type { Theme } from './theme';

/** The shipped v1 theme — refreshed dark identity (cobalt on lifted charcoal). */
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
    onAccent: palette.pureWhite,
    confHigh: palette.green,
    confMed: palette.amber,
    confLow: palette.gray600,
    warning: palette.amber,
    danger: palette.red,
    success: palette.green,
    heroGradient: [palette.cobalt, palette.cobaltSoft],
  },
};
