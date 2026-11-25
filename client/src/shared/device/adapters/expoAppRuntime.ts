import Constants from 'expo-constants';
import * as Updates from 'expo-updates';

import type { AppRuntime, UpdatesState } from '@shared/device/ports/appRuntime';

export interface ExpoSources {
  readonly constants: {
    readonly expoConfig: { readonly version?: string; readonly scheme?: string | string[] } | null;
    readonly executionEnvironment: string;
  };
  readonly updates: UpdatesState;
}

function updatesNow({ isEnabled, isEmbeddedLaunch, updateId }: UpdatesState): UpdatesState {
  return { isEnabled, isEmbeddedLaunch, updateId };
}

export function createExpoAppRuntime({ constants, updates }: ExpoSources): AppRuntime {
  return {
    manifestVersion: () => constants.expoConfig?.version,
    declaredScheme: () => constants.expoConfig?.scheme,
    executionEnvironment: () => constants.executionEnvironment,
    updates: () => updatesNow(updates),
  };
}

export const expoAppRuntime: AppRuntime = createExpoAppRuntime({
  constants: Constants,
  updates: Updates,
});
