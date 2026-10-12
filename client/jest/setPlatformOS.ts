import { Platform } from 'react-native';

import * as isWebPlatformModule from '@shared/device/isWebPlatform';

export function setPlatformOS(os: string): void {
  Platform.OS = os as typeof Platform.OS;
  jest.spyOn(isWebPlatformModule, 'isWebPlatform').mockReturnValue(os === 'web');
}
