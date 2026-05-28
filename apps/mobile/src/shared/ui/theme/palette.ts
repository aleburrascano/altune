/**
 * Raw color constants for the Altune "Midnight Studio" identity (ADR-0008).
 *
 * These are the literal locked values. Do NOT consume them directly in
 * components — go through the semantic theme (`useTheme().color.*`) so the
 * (already-defined) light theme can re-map roles without touching components.
 */
export const palette = {
  // neutrals (dark)
  black: '#0B0B0F',
  surface1: '#16161D',
  surface2: '#1E1E27',
  border: '#2A2A33',
  scrimDark: 'rgba(0,0,0,0.6)',
  // text (dark)
  white: '#F5F5F7',
  gray400: '#A1A1AD',
  gray600: '#6B6B78',
  pureWhite: '#FFFFFF',
  // brand
  indigo: '#5B6CFF',
  indigoPressed: '#4A5AE0',
  indigoTint: 'rgba(91,108,255,0.14)',
  indigoGlow: 'rgba(91,108,255,0.35)',
  magenta: '#B14CFF',
  // semantic
  green: '#3DD68C',
  amber: '#F5B544',
  red: '#FF5A5F',
} as const;
