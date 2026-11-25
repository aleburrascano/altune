import { Alert } from 'react-native';

import { createNativeDialogs } from '@shared/ui/view/dialog/nativeDialogs';

export const platformDialogs = createNativeDialogs(Alert);
