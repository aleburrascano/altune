import type { Theme } from './theme';

// AIDEV-NOTE: ADR-0008 — v1 ships DARK ONLY. This light theme exists to satisfy
// the "every color token has a light + dark variant" rule (.claude/rules/
// typescript-frontend.md) and to make light mode a config flip later. It is a
// reasonable draft, NOT visually tuned — do not ship light mode without a
// dedicated design pass.
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
    accent: '#5B6CFF',
    accentPressed: '#4A5AE0',
    accentTint: 'rgba(91,108,255,0.12)',
    accentGlow: 'rgba(91,108,255,0.30)',
    onAccent: '#FFFFFF',
    confHigh: '#1FB873',
    confMed: '#C77F12',
    confLow: '#9A9AA6',
    warning: '#C77F12',
    danger: '#D6333A',
    success: '#1FB873',
    heroGradient: ['#5B6CFF', '#B14CFF'],
  },
};
