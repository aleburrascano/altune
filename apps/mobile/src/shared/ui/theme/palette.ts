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
  // Tertiary text: lightened from gray600 so captions/placeholders clear WCAG AA
  // 4.5:1 on every dark surface (canvas/surface1/surface2). gray600 stays for
  // the low-confidence dot, where the 3:1 UI-component floor already holds.
  gray500: '#8C8C96',
  gray600: '#74747E',
  pureWhite: '#FFFFFF',
  // brand — Cobalt
  cobalt: '#2D5BFF',
  cobaltPressed: '#244BD6',
  cobaltTint: 'rgba(45,91,255,0.25)',
  cobaltSoft: '#5B82FF',
  // Accent for text/links: lightened so cobalt-on-dark clears WCAG AA 4.5:1
  // at body/caption sizes (the #2D5BFF fill is only ~3:1 as text). Fills and
  // large hero type keep `cobalt`.
  cobaltText: '#6B8FFF',
  // semantic
  green: '#3DD68C',
  amber: '#F5B544',
  red: '#FF5A5F',
} as const;
