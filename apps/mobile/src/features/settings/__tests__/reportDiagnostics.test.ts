import { Platform } from 'react-native';

import appJson from '../../../../app.json';
import { reportDiagnostics, type ReportDiagnostics } from '../reportDiagnostics';

type ReportDiagnosticsModule = { reportDiagnostics: (screen: string) => ReportDiagnostics };

function loadWithExpoConfig(
  expoConfig: Record<string, unknown> | undefined,
): ReportDiagnosticsModule {
  jest.resetModules();
  jest.doMock('expo-constants', () => ({
    __esModule: true,
    default: { expoConfig },
  }));
  return require('../reportDiagnostics') as ReportDiagnosticsModule;
}

function withPlatform<T>(overrides: { OS?: string; Version?: string | number }, run: () => T): T {
  const properties = Object.keys(overrides) as (keyof typeof overrides)[];
  const originals = properties.map((name) => [
    name,
    Object.getOwnPropertyDescriptor(Platform, name),
  ]) as [keyof typeof overrides, PropertyDescriptor | undefined][];

  for (const name of properties) {
    Object.defineProperty(Platform, name, { value: overrides[name], configurable: true });
  }
  try {
    return run();
  } finally {
    for (const [name, original] of originals) {
      if (original) Object.defineProperty(Platform, name, original);
    }
  }
}

afterEach(() => {
  jest.dontMock('expo-constants');
  jest.resetModules();
});

describe('reportDiagnostics(screen)', () => {
  it("pins app_version to app.json's expo.version when the config carries one", () => {
    expect(reportDiagnostics('settings').app_version).toBe(appJson.expo.version);
  });

  it("falls app_version back to 'dev' when expoConfig has no version", () => {
    const { reportDiagnostics: reportDiagnosticsWithoutVersion } = loadWithExpoConfig({});

    expect(reportDiagnosticsWithoutVersion('settings').app_version).toBe('dev');
  });

  it('pins platform to the mocked Platform.OS', () => {
    withPlatform({ OS: 'android' }, () => {
      expect(reportDiagnostics('settings').platform).toBe('android');
    });
  });

  it('pins os_version to the string form of a numeric Platform.Version (an Android API level)', () => {
    withPlatform({ OS: 'android', Version: 34 }, () => {
      expect(reportDiagnostics('settings').os_version).toBe('34');
    });
  });

  it('passes screen through unchanged', () => {
    expect(reportDiagnostics('library').screen).toBe('library');
  });
});
