/**
 * Theme-independent scales: spacing, radius, typography, motion, elevation.
 * Colors live in the theme (see theme.ts / darkTheme.ts) — these never change
 * between light and dark.
 */
import type { ViewStyle } from 'react-native';

/** 4pt spacing scale. */
export const spacing = {
  none: 0,
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  '2xl': 24,
  '3xl': 32,
  '4xl': 40,
  '5xl': 48,
} as const;

export const radius = {
  sm: 8,
  md: 12,
  lg: 16, // cards
  xl: 24, // sheets, nav bar
  full: 999, // pills, avatars
} as const;

/**
 * Font family keys MUST exactly match the names registered with `useFonts`
 * in the root layout. With @expo-google-fonts each weight is its own family;
 * we set `fontFamily` (never `fontWeight`) to avoid faux-bolding.
 */
export const fontFamily = {
  displaySemiBold: 'SpaceGrotesk_600SemiBold',
  displayMedium: 'SpaceGrotesk_500Medium',
  bodyRegular: 'Inter_400Regular',
  bodyMedium: 'Inter_500Medium',
  bodySemiBold: 'Inter_600SemiBold',
} as const;

export type TypographyVariant =
  | 'displayXl'
  | 'displayL'
  | 'title'
  | 'body'
  | 'bodyStrong'
  | 'label'
  | 'caption';

export const typography: Record<
  TypographyVariant,
  { fontFamily: string; fontSize: number; lineHeight: number }
> = {
  displayXl: { fontFamily: fontFamily.displaySemiBold, fontSize: 34, lineHeight: 40 },
  displayL: { fontFamily: fontFamily.displaySemiBold, fontSize: 28, lineHeight: 34 },
  title: { fontFamily: fontFamily.displayMedium, fontSize: 20, lineHeight: 26 },
  body: { fontFamily: fontFamily.bodyRegular, fontSize: 16, lineHeight: 22 },
  bodyStrong: { fontFamily: fontFamily.bodySemiBold, fontSize: 16, lineHeight: 22 },
  label: { fontFamily: fontFamily.bodyMedium, fontSize: 14, lineHeight: 18 },
  caption: { fontFamily: fontFamily.bodyMedium, fontSize: 12, lineHeight: 16 },
};

/** Motion durations (ms) — tasteful/minimal personality. */
export const duration = {
  fast: 120,
  base: 200,
  slow: 320,
} as const;

/**
 * Soft accent "glow" for active/elevated surfaces — no hard drop shadows.
 * iOS renders the colored shadow; Android falls back to a neutral elevation.
 */
export function glowStyle(color: string): ViewStyle {
  return {
    shadowColor: color,
    shadowOpacity: 1,
    shadowRadius: 16,
    shadowOffset: { width: 0, height: 0 },
    elevation: 8,
  };
}
