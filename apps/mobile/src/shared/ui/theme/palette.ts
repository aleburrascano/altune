/**
 * Raw color constants for the Altune dark identity.
 *
 * These are the literal locked values. Do NOT consume them directly in
 * components — go through the semantic theme (`useTheme().color.*`) so the
 * (already-defined) light theme can re-map roles without touching components.
 */
export const palette = {
  // neutrals (dark) — "lifted charcoal" base
  black: '#121214',
  surface1: '#1C1C20',
  surface2: '#232328',
  border: '#2A2A30',
  scrimDark: 'rgba(0,0,0,0.6)',
  // text (dark)
  white: '#F4F4F6',
  gray400: '#A6A6AE',
  gray600: '#74747E',
  pureWhite: '#FFFFFF',
  // brand — Cobalt
  cobalt: '#2D5BFF',
  cobaltPressed: '#244BD6',
  cobaltTint: 'rgba(45,91,255,0.16)',
  cobaltSoft: '#5B82FF',
  // semantic
  green: '#3DD68C',
  amber: '#F5B544',
  red: '#FF5A5F',
} as const;
