/**
 * Theme-independent scales: spacing, radius, typography, motion.
 * Colors live in the theme (see theme.ts / darkTheme.ts) — these never change
 * between light and dark.
 */

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
  displaySemiBold: 'PlusJakartaSans_700Bold',
  displayMedium: 'PlusJakartaSans_600SemiBold',
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
  { fontFamily: string; fontSize: number; lineHeight: number; letterSpacing?: number }
> = {
  displayXl: { fontFamily: fontFamily.displaySemiBold, fontSize: 34, lineHeight: 40, letterSpacing: -0.5 },
  displayL: { fontFamily: fontFamily.displaySemiBold, fontSize: 28, lineHeight: 34, letterSpacing: -0.5 },
  title: { fontFamily: fontFamily.displayMedium, fontSize: 20, lineHeight: 26 },
  body: { fontFamily: fontFamily.bodyRegular, fontSize: 16, lineHeight: 22 },
  bodyStrong: { fontFamily: fontFamily.bodySemiBold, fontSize: 16, lineHeight: 22 },
  label: { fontFamily: fontFamily.bodyMedium, fontSize: 14, lineHeight: 18 },
  caption: { fontFamily: fontFamily.bodyMedium, fontSize: 12, lineHeight: 16 },
};

/** Minimum interactive element height (WCAG AA). */
export const minInteractiveHeight = 48;

/** Motion durations (ms) — tasteful/minimal personality. */
export const duration = {
  fast: 120,
  base: 200,
  slow: 320,
} as const;
