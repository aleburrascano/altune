import { use } from 'react';

import type { Theme } from '@shared/ui/theme/theme';
import { ThemeContext } from '@shared/ui/theme/ThemeProvider';

export function useTheme(): Theme {
  return use(ThemeContext);
}
