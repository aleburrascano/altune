import React from 'react';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';

import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';

import { isLoopEnabled, setKillSwitchFileStore } from '../killSwitch';
import {
  DEFAULT_KILL_SWITCH_URL,
  KILL_SWITCH_POLL_MS,
  KILL_SWITCH_TIMEOUT_MS,
  refreshKillSwitches,
  useKillSwitchPolling,
} from '../killSwitchPoll';

const { __http } = require('../../../../jest/doubles/fetch.js');

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((_type: string, handler: AppStateChangeHandler) => {
        listeners.push(handler);
        return {
          remove: jest.fn(() => {
            listeners.splice(listeners.indexOf(handler), 1);
          }),
        };
      }),
    },
    __listeners: listeners,
  };
});

const { __listeners: appStateListeners } = jest.requireMock(
  'react-native/Libraries/AppState/AppState',
) as { __listeners: AppStateChangeHandler[] };

const SWITCH_GET = 'GET /aleburrascano/altune/main/kill-switches.json';

let warn: jest.SpyInstance;

beforeEach(() => {
  __http.reset();
  setKillSwitchFileStore(createMemoryFileStore());
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.useRealTimers();
  setKillSwitchFileStore();
  warn.mockRestore();
  delete process.env.EXPO_PUBLIC_KILL_SWITCH_URL;
});

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
}

function emitAppState(state: string): void {
  act(() => {
    [...appStateListeners].forEach((handler) => handler(state));
  });
}

describe('refreshKillSwitches', () => {
  it('fetches the switch file at the root of the public repository by default and applies it', async () => {
    expect(DEFAULT_KILL_SWITCH_URL).toBe(
      'https://raw.githubusercontent.com/aleburrascano/altune/main/kill-switches.json',
    );
    __http.reply(SWITCH_GET, { json: { sse_enabled: false } });

    await refreshKillSwitches();

    expect(__http.countFor(SWITCH_GET)).toBe(1);
    expect(__http.last().url).toBe(DEFAULT_KILL_SWITCH_URL);
    expect(isLoopEnabled('serverEvents')).toBe(false);
  });

  it('reads EXPO_PUBLIC_KILL_SWITCH_URL when a build sets it', async () => {
    process.env.EXPO_PUBLIC_KILL_SWITCH_URL = 'https://flags.example/altune.json';
    __http.reply('GET /altune.json', { json: { telemetry_enabled: false } });

    await refreshKillSwitches();

    expect(isLoopEnabled('telemetryFlush')).toBe(false);
  });

  it.each([
    ['an error status', { status: 503 }],
    ['a malformed body', { malformed: true }],
  ])('keeps the current switches on %s', async (_label, response) => {
    __http.replyOnce(SWITCH_GET, { json: { offline_downloads_enabled: false } });
    await refreshKillSwitches();
    __http.reply(SWITCH_GET, response);

    await refreshKillSwitches();

    expect(isLoopEnabled('offlineDownloads')).toBe(false);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('refresh failed'), expect.anything());
  });

  it('abandons a fetch that outlives the timeout and keeps the current switches', async () => {
    jest.useFakeTimers();
    __http.hang(SWITCH_GET);

    const done = refreshKillSwitches();
    jest.advanceTimersByTime(KILL_SWITCH_TIMEOUT_MS);
    await done;

    expect(__http.last().signal.aborted).toBe(true);
    expect(isLoopEnabled('serverEvents')).toBe(true);
  });
});

function Harness(): null {
  useKillSwitchPolling();
  return null;
}

describe('useKillSwitchPolling', () => {
  afterEach(() => {
    appStateListeners.length = 0;
  });

  it('refreshes on mount, periodically while active, and on each return to the foreground', async () => {
    jest.useFakeTimers();
    __http.replyAll({ json: {} });
    const count = (): number => __http.countFor(SWITCH_GET);

    let renderer!: ReactTestRenderer;
    act(() => {
      renderer = create(<Harness />);
    });
    await settle();
    expect(count()).toBe(1);

    act(() => jest.advanceTimersByTime(KILL_SWITCH_POLL_MS));
    await settle();
    expect(count()).toBe(2);

    emitAppState('background');
    act(() => jest.advanceTimersByTime(KILL_SWITCH_POLL_MS * 3));
    await settle();
    expect(count()).toBe(2);

    emitAppState('active');
    emitAppState('active');
    await settle();
    expect(count()).toBe(4);

    act(() => jest.advanceTimersByTime(KILL_SWITCH_POLL_MS));
    await settle();
    expect(count()).toBe(5);

    act(() => renderer.unmount());
    act(() => jest.advanceTimersByTime(KILL_SWITCH_POLL_MS * 3));
    await settle();
    expect(count()).toBe(5);
    expect(appStateListeners).toHaveLength(0);
  });

  it('lets the switches flip a loop off through the poll', async () => {
    __http.reply(SWITCH_GET, { json: { offline_downloads_enabled: false } });

    let renderer!: ReactTestRenderer;
    act(() => {
      renderer = create(<Harness />);
    });
    await act(settle);

    expect(isLoopEnabled('offlineDownloads')).toBe(false);
    act(() => renderer.unmount());
  });
});
