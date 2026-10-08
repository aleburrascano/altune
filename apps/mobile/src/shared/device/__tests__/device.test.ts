import Constants, { ExecutionEnvironment } from 'expo-constants';
import { Platform } from 'react-native';

import {
  appVersion,
  declaredAppScheme,
  deviceInfo,
  isExpoGoRuntime,
  isWebPlatform,
  webOrigin,
} from '@shared/device/device';

let mockExpoConfig: Record<string, unknown> | null = null;
let mockExecutionEnvironment = 'bare';
jest.mock('expo-constants', () => ({
  __esModule: true,
  default: {
    get expoConfig() {
      return mockExpoConfig;
    },
    get executionEnvironment() {
      return mockExecutionEnvironment;
    },
  },
  ExecutionEnvironment: { Bare: 'bare', Standalone: 'standalone', StoreClient: 'storeClient' },
}));

const originalOS = Platform.OS;

function setConfig(value: Record<string, unknown> | null): void {
  mockExpoConfig = value;
}

function setOS(os: typeof Platform.OS): void {
  Object.defineProperty(Platform, 'OS', { value: os, configurable: true });
}

afterEach(() => {
  mockExpoConfig = null;
  mockExecutionEnvironment = 'bare';
  setOS(originalOS);
  Reflect.deleteProperty(globalThis, 'window');
});

describe('appVersion', () => {
  it('returns the manifest version', () => {
    setConfig({ version: '1.2.3' });
    expect(appVersion()).toBe('1.2.3');
  });

  it('returns dev when the manifest has no version', () => {
    setConfig({});
    expect(appVersion()).toBe('dev');
  });

  it('returns dev when there is no manifest', () => {
    setConfig(null);
    expect(appVersion()).toBe('dev');
  });
});

describe('deviceInfo', () => {
  it('reports platform, stringified os version and app version', () => {
    setConfig({ version: '4.5.6' });
    setOS('android');
    expect(deviceInfo()).toEqual({
      platform: 'android',
      osVersion: String(Platform.Version),
      appVersion: '4.5.6',
    });
  });
});

describe('declaredAppScheme', () => {
  it('returns the raw scheme value', () => {
    setConfig({ scheme: ['altune', 'altune-dev'] });
    expect(declaredAppScheme()).toEqual(['altune', 'altune-dev']);
  });

  it('returns undefined when no scheme is declared', () => {
    setConfig({});
    expect(declaredAppScheme()).toBeUndefined();
  });
});

describe('webOrigin', () => {
  it('returns null on native even when a window origin exists', () => {
    Object.defineProperty(globalThis, 'window', {
      value: { location: { origin: 'https://altune.example' } },
      configurable: true,
    });
    expect(webOrigin()).toBeNull();
  });
});

describe('isWebPlatform', () => {
  it('returns true on web', () => {
    setOS('web');
    expect(isWebPlatform()).toBe(true);
  });

  it.each(['ios', 'android'] as const)('returns false on %s', (os) => {
    setOS(os);
    expect(isWebPlatform()).toBe(false);
  });

  it('reads the platform at call time', () => {
    setOS('ios');
    expect(isWebPlatform()).toBe(false);
    setOS('web');
    expect(isWebPlatform()).toBe(true);
  });
});

describe('isExpoGoRuntime', () => {
  it('returns true in the store client', () => {
    mockExecutionEnvironment = 'storeClient';
    expect(isExpoGoRuntime()).toBe(true);
  });

  it.each(['standalone', 'bare'])('returns false in %s', (env) => {
    mockExecutionEnvironment = env;
    expect(isExpoGoRuntime()).toBe(false);
  });

  it('reads the environment at call time', () => {
    expect(isExpoGoRuntime()).toBe(false);
    mockExecutionEnvironment = 'storeClient';
    expect(isExpoGoRuntime()).toBe(true);
  });

  it.each(['bare', 'standalone', 'storeClient'])('matches the isExpoGo rule in %s', (env) => {
    mockExecutionEnvironment = env;
    const live = Constants.executionEnvironment === ExecutionEnvironment.StoreClient;
    expect(isExpoGoRuntime()).toBe(live);
  });
});
