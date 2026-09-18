import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import {
  KILL_SWITCH_KEYS,
  applyKillSwitches,
  isLoopEnabled,
  onKillSwitchChange,
  setKillSwitchFileStore,
} from '../killSwitch';

const SWITCH_URI = 'memory://document/kill-switch/switches.json';

let store: MemoryFileStore;
let warn: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  setKillSwitchFileStore(store);
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  setKillSwitchFileStore();
  warn.mockRestore();
});

// Simulates a cold start: the in-memory switches are forgotten, the persisted file is kept.
function relaunch(): void {
  setKillSwitchFileStore(store);
}

function allLoops(): boolean[] {
  return [
    isLoopEnabled('serverEvents'),
    isLoopEnabled('telemetryFlush'),
    isLoopEnabled('offlineDownloads'),
    isLoopEnabled('detailEnrichment'),
  ];
}

describe('kill switches', () => {
  it('leaves every loop enabled when no switch document was ever seen', () => {
    expect(allLoops()).toEqual([true, true, true, true]);
    expect(store.files.size).toBe(0);
  });

  it('disables each loop independently by its own key', () => {
    applyKillSwitches({ sse_enabled: false });
    expect(allLoops()).toEqual([false, true, true, true]);

    applyKillSwitches({ telemetry_enabled: false });
    expect(allLoops()).toEqual([true, false, true, true]);

    applyKillSwitches({ offline_downloads_enabled: false });
    expect(allLoops()).toEqual([true, true, false, true]);

    applyKillSwitches({ detail_enrichment_enabled: false });
    expect(allLoops()).toEqual([true, true, true, false]);
  });

  it('treats a key that is absent or not a boolean false as enabled', () => {
    applyKillSwitches({ sse_enabled: false });
    applyKillSwitches({ sse_enabled: 'false', telemetry_enabled: 0 });

    expect(allLoops()).toEqual([true, true, true, true]);
  });

  it.each([null, 'off', 42, [false]])(
    'ignores a document that is not an object (%p)',
    (document) => {
      applyKillSwitches({ sse_enabled: false });

      applyKillSwitches(document);

      expect(isLoopEnabled('serverEvents')).toBe(false);
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('not an object'));
    },
  );

  it('keeps a switched-off loop off across a relaunch, before any refresh', () => {
    applyKillSwitches({ offline_downloads_enabled: false });

    relaunch();

    expect(allLoops()).toEqual([true, true, false, true]);
    expect(JSON.parse(store.files.get(SWITCH_URI) ?? '{}')).toEqual({
      schemaVersion: 1,
      [KILL_SWITCH_KEYS.serverEvents]: true,
      [KILL_SWITCH_KEYS.telemetryFlush]: true,
      [KILL_SWITCH_KEYS.offlineDownloads]: false,
      [KILL_SWITCH_KEYS.detailEnrichment]: true,
    });
  });

  it('falls back to every loop enabled when the persisted file is corrupt or not an object', () => {
    applyKillSwitches({ sse_enabled: false });
    store.files.set(SWITCH_URI, '{ not json');
    relaunch();
    expect(isLoopEnabled('serverEvents')).toBe(true);

    store.files.set(SWITCH_URI, '[false]');
    relaunch();
    expect(isLoopEnabled('serverEvents')).toBe(true);
  });

  it('keeps the switches in memory when they cannot be persisted', () => {
    const failing = createMemoryFileStore();
    failing.openDirectory = () => {
      throw new Error('disk gone');
    };
    setKillSwitchFileStore(failing);

    applyKillSwitches({ telemetry_enabled: false });

    expect(isLoopEnabled('telemetryFlush')).toBe(false);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('failed to persist'));
  });

  it('tells listeners about each loop that flipped, and only those', () => {
    const listener = jest.fn();
    const unsubscribe = onKillSwitchChange(listener);

    applyKillSwitches({ sse_enabled: false, offline_downloads_enabled: false });
    applyKillSwitches({ sse_enabled: false, offline_downloads_enabled: false });

    expect(listener.mock.calls).toEqual([
      ['serverEvents', false],
      ['offlineDownloads', false],
    ]);

    unsubscribe();
    applyKillSwitches({});
    expect(listener).toHaveBeenCalledTimes(2);
  });
});
