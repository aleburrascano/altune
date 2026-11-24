import Constants from 'expo-constants';
import * as Updates from 'expo-updates';

import type { AppRuntime } from '@shared/device/ports/appRuntime';

export const expoAppRuntime: AppRuntime = {
  manifestVersion: () => Constants.expoConfig?.version,
  declaredScheme: () => Constants.expoConfig?.scheme,
  executionEnvironment: () => Constants.executionEnvironment,
  updates: () => ({
    isEnabled: Updates.isEnabled,
    isEmbeddedLaunch: Updates.isEmbeddedLaunch,
    updateId: Updates.updateId,
  }),
};
