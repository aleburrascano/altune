import type { ConfidenceLevel, Theme } from './theme';

export function confidenceColor(theme: Theme, level: ConfidenceLevel): string {
  switch (level) {
    case 'high':
      return theme.color.confHigh;
    case 'medium':
      return theme.color.confMed;
    case 'low':
      return theme.color.confLow;
  }
}
