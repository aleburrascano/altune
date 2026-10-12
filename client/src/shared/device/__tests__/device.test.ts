import Constants, { ExecutionEnvironment } from 'expo-constants';
import { Platform } from 'react-native';

import {
  appVersion,
  appVersionLabel,
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

let mockUpdates = { isEnabled: false, isEmbeddedLaunch: true, updateId: null as string | null };
jest.mock('expo-updates', () => ({
  get isEnabled() {
    return mockUpdates.isEnabled;
  },
  get isEmbeddedLaunch() {
    return mockUpdates.isEmbeddedLaunch;
  },
  get updateId() {
    return mockUpdates.updateId;
  },
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

describe('appVersionLabel', () => {
  afterEach(() => {
    mockUpdates = { isEnabled: false, isEmbeddedLaunch: true, updateId: null };
  });

  it('names the over-the-air update the app is running', () => {
    setConfig({ version: '2.1.1' });
    mockUpdates = {
      isEnabled: true,
      isEmbeddedLaunch: false,
      updateId: '1a2b3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d',
    };
    expect(appVersionLabel()).toBe('2.1.1 · update 1a2b3c4d');
  });

  it('is the bare version when the app runs the update built into the install', () => {
    setConfig({ version: '2.1.1' });
    mockUpdates = {
      isEnabled: true,
      isEmbeddedLaunch: true,
      updateId: '1a2b3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d',
    };
    expect(appVersionLabel()).toBe('2.1.1');
  });

  it('is the bare version when updates are off', () => {
    setConfig({ version: '2.1.1' });
    expect(appVersionLabel()).toBe('2.1.1');
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
  it('returns false on native, whatever Platform.OS says', () => {
    setOS('web');
    expect(isWebPlatform()).toBe(false);
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
