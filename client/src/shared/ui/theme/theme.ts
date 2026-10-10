export type ColorScheme = 'dark' | 'light';

export type ThemeColors = {
  canvas: string;
  surface1: string;
  surface2: string;
  border: string;
  scrim: string;
  textPrimary: string;
  textSecondary: string;
  textTertiary: string;
  accent: string;
  accentPressed: string;
  accentTint: string;
  accentText: string;
  onAccent: string;
  warning: string;
  danger: string;
  success: string;
  heroGradient: readonly [string, string];
};

export type Theme = {
  scheme: ColorScheme;
  color: ThemeColors;
};

const palette = {
  black: '#121214',
  surface1: '#1C1C20',
  surface2: '#232328',
  border: '#2A2A30',
  scrimDark: 'rgba(0,0,0,0.6)',
  white: '#F4F4F6',
  gray400: '#A6A6AE',
  gray500: '#8C8C96',
  pureWhite: '#FFFFFF',
  cobalt: '#2D5BFF',
  cobaltPressed: '#244BD6',
  cobaltTint: 'rgba(45,91,255,0.25)',
  cobaltSoft: '#5B82FF',
  cobaltText: '#6B8FFF',
  green: '#3DD68C',
  amber: '#F5B544',
  red: '#FF5A5F',
} as const;

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

export const lightTheme: Theme = {
  scheme: 'light',
  color: {
    canvas: '#FFFFFF',
    surface1: '#F4F4F7',
    surface2: '#FFFFFF',
    border: '#E2E2E8',
    scrim: 'rgba(0,0,0,0.4)',
    textPrimary: '#0B0B0F',
    textSecondary: '#5B5B66',
    textTertiary: '#9A9AA6',
    accent: '#2D5BFF',
    accentPressed: '#244BD6',
    accentTint: 'rgba(45,91,255,0.12)',
    accentText: '#2D5BFF',
    onAccent: '#FFFFFF',
    warning: '#C77F12',
    danger: '#D6333A',
    success: '#1FB873',
    heroGradient: ['#2D5BFF', '#5B82FF'],
  },
};

export const themes: Record<ColorScheme, Theme> = {
  dark: darkTheme,
  light: lightTheme,
};
