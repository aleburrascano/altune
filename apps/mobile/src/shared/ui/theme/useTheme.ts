import { use } from 'react';

import type { Theme } from './theme';
import { ThemeContext } from './ThemeProvider';

/** The single way components read theme colors. Resolves to darkTheme when no
 * ThemeProvider is mounted (see ThemeProvider AIDEV-NOTE). */
export function useTheme(): Theme {
  return use(ThemeContext);
}
