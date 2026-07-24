import { use } from 'react';

import type { Theme } from './theme';
import { ThemeContext } from './ThemeProvider';

export function useTheme(): Theme {
  return use(ThemeContext);
}
