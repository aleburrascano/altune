import { palette } from './palette';
import type { Theme } from './theme';

/** The shipped v1 theme — "Midnight Studio". */
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
    textTertiary: palette.gray600,
    accent: palette.indigo,
    accentPressed: palette.indigoPressed,
    accentTint: palette.indigoTint,
    accentGlow: palette.indigoGlow,
    onAccent: palette.pureWhite,
    confHigh: palette.green,
    confMed: palette.amber,
    confLow: palette.gray600,
    warning: palette.amber,
    danger: palette.red,
    success: palette.green,
    heroGradient: [palette.indigo, palette.magenta],
  },
};
