import { createMemoryFileStore } from '@shared/files';
import { isLoopEnabled, setKillSwitchFileStore } from '@shared/killSwitch';

import { startKillSwitchPolling } from '@/mobile';

const { __http } = require('@testing/doubles/fetch.js');

type AppState = 'active' | 'background' | 'inactive';
type AppStateChangeHandler = (state: AppState) => void;

const appStateListeners: AppStateChangeHandler[] = [];

function fakeAppState(listener: AppStateChangeHandler): () => void {
  appStateListeners.push(listener);
  return () => {
    appStateListeners.splice(appStateListeners.indexOf(listener), 1);
  };
}

const DEFAULT_KILL_SWITCH_URL =
  'https://raw.githubusercontent.com/aleburrascano/altune/main/kill-switches.json';
const KILL_SWITCH_TIMEOUT_MS = 5_000;
const KILL_SWITCH_POLL_MS = 300_000;
const SWITCH_GET = 'GET /aleburrascano/altune/main/kill-switches.json';
const TWO_REFRESHES = 2;

beforeEach(() => {
  __http.reset();
  setKillSwitchFileStore(createMemoryFileStore());
});

afterEach(() => {
  jest.useRealTimers();
  setKillSwitchFileStore();
  appStateListeners.length = 0;
  delete process.env.EXPO_PUBLIC_KILL_SWITCH_URL;
});

async function refreshOnce(): Promise<void> {
  const stop = startKillSwitchPolling(fakeAppState);
  await settle();
  stop();
}

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
}

function emitAppState(state: AppState): void {
  [...appStateListeners].forEach((handler) => handler(state));
}

describe('a kill-switch refresh', () => {
  it('fetches the switch file at the root of the public repository by default and applies it', async () => {
    expect(DEFAULT_KILL_SWITCH_URL).toBe(
      'https://raw.githubusercontent.com/aleburrascano/altune/main/kill-switches.json',
    );
    __http.reply(SWITCH_GET, { json: { sse_enabled: false } });

    await refreshOnce();

    expect(__http.countFor(SWITCH_GET)).toBe(1);
    expect(__http.last().url).toBe(DEFAULT_KILL_SWITCH_URL);
    expect(isLoopEnabled('serverEvents')).toBe(false);
  });

  it('reads EXPO_PUBLIC_KILL_SWITCH_URL when a build sets it', async () => {
    process.env.EXPO_PUBLIC_KILL_SWITCH_URL = 'https://flags.example/altune.json';
    __http.reply('GET /altune.json', { json: { telemetry_enabled: false } });

    await refreshOnce();

    expect(isLoopEnabled('telemetryFlush')).toBe(false);
  });

  it.each([
    ['an error status', { status: 503 }],
    ['a malformed body', { malformed: true }],
  ])('keeps the current switches on %s', async (_label, response) => {
    __http.replyOnce(SWITCH_GET, { json: { offline_downloads_enabled: false } });
    await refreshOnce();
    __http.reply(SWITCH_GET, response);

    await refreshOnce();

    expect(__http.countFor(SWITCH_GET)).toBe(TWO_REFRESHES);
    expect(isLoopEnabled('offlineDownloads')).toBe(false);
  });

  it('abandons a fetch that outlives the timeout and keeps the current switches', async () => {
    jest.useFakeTimers();
    __http.hang(SWITCH_GET);

    const stop = startKillSwitchPolling(fakeAppState);
    jest.advanceTimersByTime(KILL_SWITCH_TIMEOUT_MS);
    await settle();
    stop();

    expect(__http.last().signal.aborted).toBe(true);
    expect(isLoopEnabled('serverEvents')).toBe(true);
  });
});

describe('startKillSwitchPolling', () => {
  it('refreshes at start, periodically while active, and on each return to the foreground', async () => {
    jest.useFakeTimers();
    __http.replyAll({ json: {} });
    const count = (): number => __http.countFor(SWITCH_GET);

    const stop = startKillSwitchPolling(fakeAppState);
    await settle();
    expect(count()).toBe(1);

    jest.advanceTimersByTime(KILL_SWITCH_POLL_MS);
    await settle();
    expect(count()).toBe(2);

    emitAppState('background');
    emitAppState('inactive');
    jest.advanceTimersByTime(KILL_SWITCH_POLL_MS * 3);
    await settle();
    expect(count()).toBe(2);

    emitAppState('active');
    emitAppState('active');
    await settle();
    expect(count()).toBe(4);

    jest.advanceTimersByTime(KILL_SWITCH_POLL_MS);
    await settle();
    expect(count()).toBe(5);

    stop();
    jest.advanceTimersByTime(KILL_SWITCH_POLL_MS * 3);
    await settle();
    expect(count()).toBe(5);
    expect(appStateListeners).toHaveLength(0);
  });

  it('does not refresh on a return to the foreground after the stop function ran', async () => {
    __http.replyAll({ json: {} });

    const stop = startKillSwitchPolling(fakeAppState);
    await settle();
    stop();
    const before = __http.countFor(SWITCH_GET);

    emitAppState('active');
    await settle();

    expect(__http.countFor(SWITCH_GET)).toBe(before);
  });

  it('lets the switches flip a loop off through the poll', async () => {
    __http.reply(SWITCH_GET, { json: { offline_downloads_enabled: false } });

    const stop = startKillSwitchPolling(fakeAppState);
    await settle();

    expect(isLoopEnabled('offlineDownloads')).toBe(false);
    stop();
  });
});
