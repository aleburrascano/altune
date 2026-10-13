export const spacing = {
  none: 0,
  '2xs': 2,
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
  lg: 16,
  xl: 24,
  full: 999,
} as const;

export const fontFamily = {
  displaySemiBold: 'PlusJakartaSans_700Bold',
  displayMedium: 'PlusJakartaSans_600SemiBold',
  bodyRegular: 'Inter_400Regular',
  bodyMedium: 'Inter_500Medium',
  bodySemiBold: 'Inter_600SemiBold',
} as const;

export type TypographyVariant =
  'displayL' | 'editorial' | 'title' | 'body' | 'bodyStrong' | 'label' | 'caption' | 'overline';

export const typography: Record<
  TypographyVariant,
  { fontFamily: string; fontSize: number; lineHeight: number; letterSpacing?: number }
> = {
  displayL: {
    fontFamily: fontFamily.displaySemiBold,
    fontSize: 28,
    lineHeight: 34,
    letterSpacing: -0.5,
  },
  editorial: {
    fontFamily: fontFamily.displaySemiBold,
    fontSize: 34,
    lineHeight: 38,
    letterSpacing: -1,
  },
  title: { fontFamily: fontFamily.displayMedium, fontSize: 20, lineHeight: 26 },
  body: { fontFamily: fontFamily.bodyRegular, fontSize: 16, lineHeight: 22 },
  bodyStrong: { fontFamily: fontFamily.bodySemiBold, fontSize: 16, lineHeight: 22 },
  label: { fontFamily: fontFamily.bodyMedium, fontSize: 14, lineHeight: 18 },
  caption: { fontFamily: fontFamily.bodyMedium, fontSize: 12, lineHeight: 16 },
  overline: {
    fontFamily: fontFamily.bodySemiBold,
    fontSize: 10,
    lineHeight: 13,
    letterSpacing: 0.8,
  },
};

export const minInteractiveHeight = 48;

const TWO = 2;

export function halved(size: number) {
  return size / TWO;
}

export function doubled(size: number) {
  return size * TWO;
}

export const borderWidth = { emphasis: 2 } as const;

export const opacity = { pressed: 0.6, pressedSubtle: 0.85, dimmed: 0.7, disabled: 0.5 } as const;

export const touchTarget = 44;

export const sheetGrabber = { width: 36, height: 4, borderRadius: 2 } as const;

export const sheetTopRadius = 20;

export const pillPaddingVertical = 10;

export const zLayer = { searchField: 10, suggestions: 20 } as const;

export const shadow = { cover: '0px 8px 24px rgba(0, 0, 0, 0.45)' } as const;
