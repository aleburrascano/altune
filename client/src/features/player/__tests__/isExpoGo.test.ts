import type * as Device from '@shared/device';
import type * as Player from '@features/player';

function runtimeIn(executionEnvironment: string): Device.AppRuntime {
  return {
    manifestVersion: () => undefined,
    declaredScheme: () => undefined,
    executionEnvironment: () => executionEnvironment,
    updates: () => ({ isEnabled: false, isEmbeddedLaunch: true, updateId: null }),
  };
}

function loadIsExpoGo(executionEnvironment: string): boolean {
  let isExpoGo: boolean | undefined;
  jest.isolateModules(() => {
    const { installAppRuntime } = require('@shared/device') as typeof Device;
    installAppRuntime(runtimeIn(executionEnvironment));
    ({ isExpoGo } = require('@features/player') as typeof Player);
  });
  return isExpoGo as boolean;
}

describe('isExpoGo', () => {
  it('is true when the execution environment is the Expo Go store client', () => {
    expect(loadIsExpoGo('storeClient')).toBe(true);
  });

  it('is false when the execution environment is a standalone build', () => {
    expect(loadIsExpoGo('standalone')).toBe(false);
  });
});
