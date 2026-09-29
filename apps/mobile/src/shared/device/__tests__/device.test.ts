import { Platform } from 'react-native';

import { appVersion, declaredAppScheme, deviceInfo, webOrigin } from '../device';

let mockExpoConfig: Record<string, unknown> | null = null;
jest.mock('expo-constants', () => ({
  __esModule: true,
  default: {
    get expoConfig() {
      return mockExpoConfig;
    },
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
  it('returns null on native', () => {
    setOS('ios');
    expect(webOrigin()).toBeNull();
  });

  it('returns the window origin on web', () => {
    setOS('web');
    Object.defineProperty(globalThis, 'window', {
      value: { location: { origin: 'https://altune.example' } },
      configurable: true,
    });
    expect(webOrigin()).toBe('https://altune.example');
  });

  it('returns null on web when there is no window', () => {
    setOS('web');
    expect(webOrigin()).toBeNull();
  });

  it('returns null on web when window has no location', () => {
    setOS('web');
    Object.defineProperty(globalThis, 'window', { value: {}, configurable: true });
    expect(webOrigin()).toBeNull();
  });
});
