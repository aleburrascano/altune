function loadIsExpoGo(executionEnvironment: string): boolean {
  let isExpoGo: boolean | undefined;
  jest.isolateModules(() => {
    jest.doMock('expo-constants', () => ({
      __esModule: true,
      default: { executionEnvironment },
      ExecutionEnvironment: { Bare: 'bare', Standalone: 'standalone', StoreClient: 'storeClient' },
    }));
    ({ isExpoGo } = require('../isExpoGo') as { isExpoGo: boolean });
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
