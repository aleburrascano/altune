import { ExecutionEnvironment } from 'expo-constants';

import appJson from '@/../app.json';
import {
  type AppRuntime,
  type ExpoSources,
  createExpoAppRuntime,
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

describe('the expo adapter over the sources expo gives it', () => {
  const NO_UPDATES = { isEnabled: false, isEmbeddedLaunch: true, updateId: null };

  function adapterOver(sources: Partial<ExpoSources['constants']> = {}): AppRuntime {
    return createExpoAppRuntime({
      constants: { expoConfig: null, executionEnvironment: 'bare', ...sources },
      updates: NO_UPDATES,
    });
  }

  it('reports no version and no scheme when the app has no expo config', () => {
    const runtime = adapterOver({ expoConfig: null });
    expect(runtime.manifestVersion()).toBeUndefined();
    expect(runtime.declaredScheme()).toBeUndefined();
  });

  it('reports the version and scheme the config declares', () => {
    const runtime = adapterOver({ expoConfig: { version: '3.1.4', scheme: ['a', 'b'] } });
    expect(runtime.manifestVersion()).toBe('3.1.4');
    expect(runtime.declaredScheme()).toEqual(['a', 'b']);
  });

  it('reports the execution environment expo names', () => {
    expect(adapterOver({ executionEnvironment: 'storeClient' }).executionEnvironment()).toBe(
      'storeClient',
    );
  });

  it('reports the updates state expo holds', () => {
    const updates = { isEnabled: true, isEmbeddedLaunch: false, updateId: 'abc' };
    const runtime = createExpoAppRuntime({
      constants: { expoConfig: null, executionEnvironment: 'bare' },
      updates,
    });
    expect(runtime.updates()).toEqual(updates);
  });
});
