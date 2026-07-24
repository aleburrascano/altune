export type ColorScheme = 'dark' | 'light';

export type ConfidenceLevel = 'high' | 'medium' | 'low';

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
  confHigh: string;
  confMed: string;
  confLow: string;
  warning: string;
  danger: string;
  success: string;
  heroGradient: readonly [string, string];
};

export type Theme = {
  scheme: ColorScheme;
  color: ThemeColors;
};
