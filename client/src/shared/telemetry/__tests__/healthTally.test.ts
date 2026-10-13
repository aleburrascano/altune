import { signInTestUser } from '@testing/signInTestUser';

import { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/healthTally';

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((type: string, handler: AppStateChangeHandler) => {
        if (type === 'change') listeners.push(handler);
        return { remove: jest.fn() };
      }),
    },
    __listeners: listeners,
  };
});

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const HTTP_ACCEPTED = 202;
const SETTLE_TICKS = 5;

const KEYS = ['a_ok', 'a_failed', 'b_ok'] as const;

type Key = (typeof KEYS)[number];

function appStateListeners(): AppStateChangeHandler[] {
  return (
    jest.requireMock('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    }
  ).__listeners;
}

function buildTally(onFlushFailure: (error: unknown) => void = () => undefined) {
  return createHealthTally<Key>({ eventType: 'detail_health', keys: KEYS, onFlushFailure });
}

async function settle(): Promise<void> {
  for (let i = 0; i < SETTLE_TICKS; i++) await new Promise((resolve) => setImmediate(resolve));
}

type SentEvent = { type: string; payload: Record<string, unknown> };

async function sentEvents(): Promise<SentEvent[]> {
  await settle();
  return (__http.requests as { path: string; method: string; body: string }[])
    .filter((request) => request.method === 'POST' && request.path === '/v1/discovery/events')
    .map((request) => {
      const event = JSON.parse(request.body) as {
        type: string;
        payload: Record<string, unknown>;
      };
      const counts = Object.fromEntries(
        Object.entries(event.payload).filter(([key]) => key !== 'session_id'),
      );
      return { type: event.type, payload: counts };
    });
}

beforeEach(async () => {
  appStateListeners().length = 0;
  __http.reply(EVENTS, { status: HTTP_ACCEPTED });
  await signInTestUser();
});

describe('createHealthTally', () => {
  it('sends nothing when no outcome was counted', async () => {
    buildTally().flush();

    expect(await sentEvents()).toEqual([]);
  });

  it('sends every key with its count, zeros included, under the event type', async () => {
    const tally = buildTally();
    tally.count('a_ok');
    tally.count('a_ok');
    tally.count('b_ok');

    tally.flush();

    expect(await sentEvents()).toEqual([
      { type: 'detail_health', payload: { a_ok: 2, a_failed: 0, b_ok: 1 } },
    ]);
  });

  it('starts a fresh tally after a flush', async () => {
    const tally = buildTally();
    tally.count('a_ok');
    tally.flush();
    tally.count('b_ok');

    tally.flush();

    expect(await sentEvents()).toEqual([
      { type: 'detail_health', payload: { a_ok: 1, a_failed: 0, b_ok: 0 } },
      { type: 'detail_health', payload: { a_ok: 0, a_failed: 0, b_ok: 1 } },
    ]);
  });

  it('sends on its own once a full batch has been counted', async () => {
    const tally = buildTally();

    for (let i = 0; i < HEALTH_BATCH - 1; i++) tally.count('a_failed');
    expect(await sentEvents()).toEqual([]);
    tally.count('a_failed');

    expect(await sentEvents()).toEqual([
      { type: 'detail_health', payload: { a_ok: 0, a_failed: HEALTH_BATCH, b_ok: 0 } },
    ]);
  });

  it('drops the counted outcomes on reset', async () => {
    const tally = buildTally();
    tally.count('a_ok');

    tally.reset();
    tally.flush();

    expect(await sentEvents()).toEqual([]);
  });

  it('sends the partial batch when the app goes to the background, not for other states', async () => {
    const tally = buildTally();
    tally.count('a_ok');

    for (const listener of appStateListeners()) listener('active');
    expect(await sentEvents()).toEqual([]);
    for (const listener of appStateListeners()) listener('background');

    expect(await sentEvents()).toHaveLength(1);
  });

  it('subscribes to the app state once however many outcomes are counted', () => {
    const tally = buildTally();

    tally.count('a_ok');
    tally.count('a_failed');
    tally.count('b_ok');

    expect(appStateListeners()).toHaveLength(1);
  });

  it('hands a failed send to onFlushFailure', async () => {
    __http.reset();
    __http.fail(EVENTS);
    const onFlushFailure = jest.fn();
    const tally = buildTally(onFlushFailure);
    tally.count('a_ok');

    tally.flush();
    await settle();

    expect(onFlushFailure).toHaveBeenCalledTimes(1);
  });
});
