import {
  type AppRuntime,
  type UpdatesState,
  appVersion,
  appVersionLabel,
  declaredAppScheme,
  installAppRuntime,
  installBuild,
  isExpoGoRuntime,
  isWebPlatform,
  platformName,
  telemetryVersion,
  webOrigin,
} from '@shared/device';

const RUNNING_UPDATE_ID = '1a2b3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d';
const BUILD_COMMIT = '0123456789abcdef0123';

const UPDATES_OFF: UpdatesState = { isEnabled: false, isEmbeddedLaunch: true, updateId: null };

const RUNNING_AN_UPDATE: UpdatesState = {
  isEnabled: true,
  isEmbeddedLaunch: false,
  updateId: RUNNING_UPDATE_ID,
};

function runtimeWith(facts: Partial<AppRuntime>): AppRuntime {
  return {
    manifestVersion: () => undefined,
    declaredScheme: () => undefined,
    executionEnvironment: () => 'bare',
    updates: () => UPDATES_OFF,
    ...facts,
  };
}

afterEach(() => {
  installAppRuntime();
  Reflect.deleteProperty(globalThis, 'window');
});

describe('appVersion', () => {
  it('returns the manifest version', () => {
    installAppRuntime(runtimeWith({ manifestVersion: () => '1.2.3' }));
    expect(appVersion()).toBe('1.2.3');
  });

  it('returns dev when the manifest has no version', () => {
    installAppRuntime(runtimeWith({}));
    expect(appVersion()).toBe('dev');
  });
});

describe('appVersionLabel', () => {
  it('names the over-the-air update the app is running', () => {
    installAppRuntime(
      runtimeWith({ manifestVersion: () => '2.1.1', updates: () => RUNNING_AN_UPDATE }),
    );
    expect(appVersionLabel()).toBe('2.1.1 · update 1a2b3c4d');
  });

  it('is the bare version when the app runs the update built into the install', () => {
    installAppRuntime(
      runtimeWith({
        manifestVersion: () => '2.1.1',
        updates: () => ({ ...RUNNING_AN_UPDATE, isEmbeddedLaunch: true }),
      }),
    );
    expect(appVersionLabel()).toBe('2.1.1');
  });

  it('is the bare version when updates are off', () => {
    installAppRuntime(
      runtimeWith({
        manifestVersion: () => '2.1.1',
        updates: () => ({ ...RUNNING_AN_UPDATE, isEnabled: false }),
      }),
    );
    expect(appVersionLabel()).toBe('2.1.1');
  });
});

describe('telemetryVersion', () => {
  it('joins the short build commit to the version label', () => {
    installAppRuntime(
      runtimeWith({ manifestVersion: () => '2.1.1', updates: () => RUNNING_AN_UPDATE }),
    );
    installBuild({ platform: 'android', commit: BUILD_COMMIT });
    expect(telemetryVersion()).toBe('0123456789ab/2.1.1 · update 1a2b3c4d');
  });

  it.each([[undefined], ['']])('says dev when the build has commit %p', (commit) => {
    installAppRuntime(runtimeWith({ manifestVersion: () => '2.1.1' }));
    installBuild({ platform: 'android', commit });
    expect(telemetryVersion()).toBe('dev/2.1.1');
  });
});

describe('platformName', () => {
  it('is unknown before the app installs its build', () => {
    let platformBeforeInstall = '';
    jest.isolateModules(() => {
      platformBeforeInstall = require('@shared/device').platformName();
    });
    expect(platformBeforeInstall).toBe('unknown');
  });

  it('is the platform the build was installed with', () => {
    installBuild({ platform: 'android', commit: undefined });
    expect(platformName()).toBe('android');
  });
});

describe('declaredAppScheme', () => {
  it('returns the raw scheme value', () => {
    installAppRuntime(runtimeWith({ declaredScheme: () => ['altune', 'altune-dev'] }));
    expect(declaredAppScheme()).toEqual(['altune', 'altune-dev']);
  });

  it('returns undefined when no scheme is declared', () => {
    installAppRuntime(runtimeWith({}));
    expect(declaredAppScheme()).toBeUndefined();
  });
});

describe('isExpoGoRuntime', () => {
  it('is true in the store client', () => {
    installAppRuntime(runtimeWith({ executionEnvironment: () => 'storeClient' }));
    expect(isExpoGoRuntime()).toBe(true);
  });

  it.each(['standalone', 'bare'])('is false in %s', (environment) => {
    installAppRuntime(runtimeWith({ executionEnvironment: () => environment }));
    expect(isExpoGoRuntime()).toBe(false);
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
  it('returns false on native', () => {
    expect(isWebPlatform()).toBe(false);
  });
});
