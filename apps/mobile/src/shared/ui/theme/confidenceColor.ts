import type { ConfidenceLevel, Theme } from './theme';

/** Maps a discovery confidence level to its semantic theme color. Kept off the
 * brand accent so indigo always means "interactive", never "data". */
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
