import { ExecutionEnvironment } from 'expo-constants';

import appJson from '@/../app.json';
import {
  type AppRuntime,
  appVersion,
  appVersionLabel,
  declaredAppScheme,
  installAppRuntime,
  isExpoGoRuntime,
} from '@shared/device';

const appConfig = appJson.expo;

const JEST_EXPO_UPDATE_ID = 'mock';

const runtimeOverTheAppConfig: AppRuntime = {
  manifestVersion: () => appConfig.version,
  declaredScheme: () => appConfig.scheme,
  executionEnvironment: () => 'bare',
  updates: () => ({ isEnabled: true, isEmbeddedLaunch: false, updateId: JEST_EXPO_UPDATE_ID }),
};

afterEach(() => {
  installAppRuntime();
});

describe.each([
  ['the expo runtime', undefined],
  ['the fake runtime tests install', runtimeOverTheAppConfig],
])('AppRuntime contract: %s, in a development build of this app', (_name, runtime) => {
  beforeEach(() => {
    installAppRuntime(runtime);
  });

  it('reports the version app.json declares', () => {
    expect(appVersion()).toBe(appConfig.version);
  });

  it('labels the version with the update jest-expo reports running', () => {
    expect(appVersionLabel()).toBe(`${appConfig.version} · update ${JEST_EXPO_UPDATE_ID}`);
  });

  it('reports the scheme app.json declares', () => {
    expect(declaredAppScheme()).toEqual(appConfig.scheme);
  });

  it('is not Expo Go', () => {
    expect(isExpoGoRuntime()).toBe(false);
  });
});

describe('AppRuntime contract: Expo Go', () => {
  afterEach(() => {
    installAppRuntime();
  });

  it('is the store client environment expo-constants names', () => {
    installAppRuntime({
      ...runtimeOverTheAppConfig,
      executionEnvironment: () => ExecutionEnvironment.StoreClient,
    });
    expect(isExpoGoRuntime()).toBe(true);
  });
});
