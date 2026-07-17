/** Semantic theme contract. Every color is a role, not a raw hue. */

export type ColorScheme = 'dark' | 'light';

/** Matches the wire `DiscoveryConfidence` string values without coupling the
 * design system to the api-client. */
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
  /** Accent for text/links — lighter than `accent` so it clears AA on dark. */
  accentText: string;
  /** Foreground (text/icon) color on top of an accent fill. */
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
