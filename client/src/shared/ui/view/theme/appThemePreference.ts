import { defaultFileStore } from '@shared/files';
import { warn } from '@shared/log';

import { createThemePreference } from '@shared/ui/view/theme/themePreference';

export const useThemePreference = createThemePreference({ files: defaultFileStore, log: { warn } });
