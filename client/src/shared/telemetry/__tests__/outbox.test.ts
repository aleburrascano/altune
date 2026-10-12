import * as Crypto from 'expo-crypto';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { signInTestUser } from '@testing/signInTestUser';

import type { OutboxEntry } from '@shared/telemetry/outbox';
import {
  enqueueCritical,
  flushOutbox,
  clearOutbox,
  droppedCriticalCount,
  flushBackoffMs,
  FLUSH_BACKOFF_BASE_MS,
  FLUSH_BACKOFF_CAP_MS,
  setOutboxOwner,
  _resetOutboxForTest,
  capEntries,
  capOutbox,
  dedupeById,
  withEnvelope,
} from '@shared/telemetry/outbox';
import {
  loadPersistedOutbox,
  persistOutbox,
  setOutboxFileStore,
} from '@shared/telemetry/outboxStore';
import type { DiscoveryEvent } from '@shared/telemetry/recordEvent';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

const { fakeFetch, FakeResponse, __http } = require('@testing/doubles/fetch.js');

type AppStateChangeHandler = (state: string) => void;

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

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

type Reply = number | 'unreachable';
type Responder = (entry: OutboxEntry) => Reply | Promise<Reply>;

const EVENTS_PATH = '/v1/discovery/events';
const HTTP_ACCEPTED = 202;
const HTTP_BAD_REQUEST = 400;
const HTTP_UNAUTHORIZED = 401;
const HTTP_SERVER_ERROR = 500;
const HTTP_UNAVAILABLE = 503;
const OVERFLOW = 2;
const SECOND_ATTEMPT = 2;
const FOURTH_ATTEMPT = 4;
const SETTLE_TICKS = 500;
const HTTP_TOO_MANY_REQUESTS = 429;

function acceptAll(): Reply {
  return HTTP_ACCEPTED;
}

let received: OutboxEntry[] = [];
let alwaysReply: Responder = acceptAll;
let onceReplies: Responder[] = [];

function serverReplies(reply: Reply | Responder): void {
  alwaysReply = typeof reply === 'function' ? reply : () => reply;
}

function serverRepliesOnce(reply: Reply | Responder): void {
  onceReplies.push(typeof reply === 'function' ? reply : () => reply);
}

function holdNextRequest(): () => void {
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  serverRepliesOnce(async () => {
    await released;
    return HTTP_ACCEPTED;
  });
  return release;
}

function resetServer(): void {
  received = [];
  alwaysReply = acceptAll;
  onceReplies = [];
}

async function fakeServerFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  if (init?.method !== 'POST' || !String(input).endsWith(EVENTS_PATH)) {
    return fakeFetch(input, init);
  }
  const entry = JSON.parse(String(init.body)) as OutboxEntry;
  received.push(entry);
  const scripted = entry.type === 'outbox_flush_failed' ? undefined : onceReplies.shift();
  const reply = await (scripted ?? alwaysReply)(entry);
  if (reply === 'unreachable') throw __http.transportError();
  return new FakeResponse({ status: reply });
}

async function settle(): Promise<void> {
  for (let i = 0; i < SETTLE_TICKS; i += 1) await Promise.resolve();
}

const MAX_ENTRIES = 50;

function event(overrides: Partial<DiscoveryEvent> = {}): DiscoveryEvent {
  return { type: 'library_add', ...overrides };
}

function sentEntries(): OutboxEntry[] {
  return received.filter((entry) => entry.type !== 'outbox_flush_failed');
}

function lastPersisted(): readonly OutboxEntry[] {
  return loadPersistedOutbox();
}

function restoreFromDisk(entries: OutboxEntry[]): void {
  persistOutbox(entries);
  _resetOutboxForTest({ restored: false });
}

beforeEach(() => {
  jest.useFakeTimers();
  (
    jest.requireMock('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    }
  ).__listeners.length = 0;
  setOutboxFileStore(createMemoryFileStore());
  _resetOutboxForTest();
  resetServer();
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  global.fetch = fakeServerFetch;
  void signInTestUser();
});

afterEach(() => {
  jest.restoreAllMocks();
  setOutboxFileStore();
  _resetOutboxForTest();
  jest.useRealTimers();
});

describe('remote kill switch', () => {
  type AppStateChangeHandler = (state: string) => void;

  const { __listeners: appStateListeners } = jest.requireMock(
    'react-native/Libraries/AppState/AppState',
  ) as { __listeners: AppStateChangeHandler[] };

  function queuedIds(): (string | undefined)[] {
    return lastPersisted().map((e) => e.search_id);
  }

  async function settle(): Promise<void> {
    for (let i = 0; i < 20; i += 1) await Promise.resolve();
  }

  beforeEach(() => {
    jest.useFakeTimers();
    setKillSwitchFileStore(createMemoryFileStore());
    _resetOutboxForTest();
    resetServer();
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    _resetOutboxForTest();
    setKillSwitchFileStore();
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  describe('outbox — remote kill switch', () => {
    it('sends nothing while the switch is off and keeps the entries queued', async () => {
      applyKillSwitches({ telemetry_enabled: false });

      await enqueueCritical({ type: 'library_add', search_id: 'a' });
      await flushOutbox();
      appStateListeners.forEach((handler) => handler('active'));
      await settle();

      expect(sentEntries()).toHaveLength(0);
      expect(queuedIds()).toEqual(['a']);
    });

    it('arms no retry timer while the switch is off', async () => {
      serverReplies(HTTP_UNAVAILABLE);
      await enqueueCritical({ type: 'library_add', search_id: 'a' });
      expect(sentEntries()).toHaveLength(1);

      applyKillSwitches({ telemetry_enabled: false });
      await jest.advanceTimersByTimeAsync(10 * 60 * 1000);

      expect(sentEntries()).toHaveLength(1);
      expect(jest.getTimerCount()).toBe(0);
    });

    it('stops a running pass before its next entry when the switch is turned off', async () => {
      const releaseFirst = holdNextRequest();
      restoreFromDisk(
        ['a', 'b'].map((id) => ({
          type: 'library_add',
          search_id: id,
          event_id: `evt-${id}`,
          client_occurred_at: '2026-09-15T00:00:00.000Z',
        })),
      );
      const pass = flushOutbox();
      await settle();
      expect(received).toHaveLength(1);

      applyKillSwitches({ telemetry_enabled: false });
      releaseFirst();
      await pass;

      expect(sentEntries().map((e) => e.search_id)).toEqual(['a']);
      expect(queuedIds()).toEqual(['b']);
    });

    it('flushes what was held back as soon as the switch is turned back on', async () => {
      applyKillSwitches({ telemetry_enabled: false });
      await enqueueCritical({ type: 'library_add', search_id: 'a' });

      applyKillSwitches({ telemetry_enabled: true });
      await settle();

      expect(sentEntries().map((e) => e.search_id)).toEqual(['a']);
      expect(queuedIds()).toEqual([]);
    });
  });
});

describe('Reducer: enqueueCritical', () => {
  it('enqueuing onto an empty queue commits the new entry to disk before its send has resolved', () => {
    serverReplies(() => new Promise<Reply>(() => undefined));

    enqueueCritical(event({ search_id: 'first' })).catch(() => undefined);

    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['first']);
  });

  it('enqueuing past MAX_ENTRIES caps the queue at MAX_ENTRIES, dropping the oldest entry', async () => {
    serverReplies(HTTP_UNAVAILABLE);

    for (let i = 0; i <= MAX_ENTRIES; i += 1) {
      await enqueueCritical(event({ search_id: `q${i}` }));
    }

    const persisted = lastPersisted();
    expect(persisted).toHaveLength(MAX_ENTRIES);
    expect(persisted?.some((e) => e.search_id === 'q0')).toBe(false);
    expect(persisted?.some((e) => e.search_id === `q${MAX_ENTRIES}`)).toBe(true);
  });

  it('enqueuing an entry whose minted event_id collides with one already queued overwrites it rather than duplicating it', async () => {
    const mintSpy = jest
      .spyOn(Crypto, 'randomUUID')
      .mockReturnValue('11111111-1111-4111-8111-111111111111');
    serverReplies(HTTP_UNAVAILABLE);

    await enqueueCritical(event({ search_id: 'first' }));
    await enqueueCritical(event({ search_id: 'second' }));
    mintSpy.mockRestore();

    const persisted = lastPersisted();
    expect(persisted).toHaveLength(1);
    expect(persisted?.[0]?.search_id).toBe('second');
  });
});

describe('Concurrency: an entry enqueued during an in-flight flush', () => {
  it('is sent by the same flush cycle with no further trigger', async () => {
    const releaseFirst = holdNextRequest();

    const first = enqueueCritical(event({ search_id: 'first' }));
    const second = enqueueCritical(event({ search_id: 'second' }));
    releaseFirst();
    await Promise.all([first, second]);

    expect(sentEntries().map((e) => e.search_id)).toEqual(['first', 'second']);
    expect(lastPersisted()).toEqual([]);
  });

  it('never runs two sends concurrently while draining the late entries', async () => {
    let inFlight = 0;
    let maxInFlight = 0;
    serverReplies(async () => {
      inFlight += 1;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await Promise.resolve();
      inFlight -= 1;
      return HTTP_ACCEPTED;
    });

    await Promise.all([
      enqueueCritical(event({ search_id: 'a' })),
      enqueueCritical(event({ search_id: 'b' })),
      enqueueCritical(event({ search_id: 'c' })),
    ]);

    expect(sentEntries()).toHaveLength(3);
    expect(maxInFlight).toBe(1);
  });

  it('does not re-send a retryable failure in a tight loop', async () => {
    serverReplies(HTTP_UNAVAILABLE);

    await enqueueCritical(event({ search_id: 'a' }));

    expect(sentEntries()).toHaveLength(1);
  });

  it('stops at an offline outcome without retrying the entry in the same pass', async () => {
    serverReplies('unreachable');

    await enqueueCritical(event({ search_id: 'a' }));

    expect(sentEntries()).toHaveLength(1);
  });
});

describe('Backpressure: shedding a label-critical entry at the cap is recorded, never silent', () => {
  it('records the drop when enqueuing past MAX_ENTRIES sheds the oldest label-critical entry', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    for (let i = 0; i <= MAX_ENTRIES; i += 1) {
      await enqueueCritical(event({ search_id: `q${i}` }));
    }

    expect(lastPersisted()).toHaveLength(MAX_ENTRIES);
    expect(droppedCriticalCount()).toBe(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('label-critical'));

    warn.mockRestore();
  });

  it('does not record a drop while the queue stays within the cap', async () => {
    serverReplies(HTTP_UNAVAILABLE);

    for (let i = 0; i < MAX_ENTRIES; i += 1) {
      await enqueueCritical(event({ search_id: `q${i}` }));
    }

    expect(droppedCriticalCount()).toBe(0);
  });

  it('records a plural drop when a restored outbox exceeds the cap by more than one', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    serverReplies(HTTP_UNAVAILABLE);
    restoreFromDisk(
      Array.from({ length: MAX_ENTRIES + OVERFLOW }, (_, i) => ({
        ...event({ search_id: `q${i}` }),
        event_id: `persisted-${i}`,
        client_occurred_at: '2026-01-01T00:00:00.000Z',
      })),
    );

    await flushOutbox();

    expect(droppedCriticalCount()).toBe(OVERFLOW);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('entries at cap'));

    warn.mockRestore();
  });
});

describe('Security: clearOutbox drops queued telemetry on sign-out / account switch', () => {
  it('empties the in-memory queue and deletes the persisted outbox on disk', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'user-a-report' }));
    expect(lastPersisted()).toHaveLength(1);

    clearOutbox();

    expect(lastPersisted()).toEqual([]);
  });

  it("a flush after clear sends nothing, so user A's queued entry cannot be delivered under user B's session", async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'user-a-report' }));

    clearOutbox();
    resetServer();
    await flushOutbox();

    expect(sentEntries()).toHaveLength(0);
  });

  it('runs off the sign-out registry, so no caller in auth has to reach for it', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'user-a-report' }));

    runSignOutCleanups();

    expect(lastPersisted()).toEqual([]);
  });
});

describe('Reducer: flushOutbox', () => {
  it('flushing an empty queue sends nothing and writes nothing to disk', async () => {
    await flushOutbox();

    expect(sentEntries()).toHaveLength(0);
    expect(lastPersisted()).toEqual([]);
  });

  it('a full drain sends every queued entry once, in order, and ends with an empty queue on disk', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    resetServer();

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b']);
    expect(lastPersisted()).toEqual([]);
  });
});

describe('Reducer: ensureRestored (cold start, driven through a fresh module instance)', () => {
  it('merges the persisted outbox into memory on first use and dedupes a corrupted duplicate event_id, keeping the later record', async () => {
    restoreFromDisk([
      { type: 'library_add', event_id: 'disk-1', client_occurred_at: 'stale' },
      { type: 'library_add', event_id: 'disk-1', client_occurred_at: 'fresh' },
      { type: 'wrong_album', event_id: 'disk-2', client_occurred_at: 't2' },
    ]);

    await flushOutbox();

    expect(received.map((entry) => entry.event_id)).toEqual(['disk-1', 'disk-2']);
    expect(received[0]?.client_occurred_at).toBe('fresh');
    expect(lastPersisted()).toEqual([]);
  });
});

describe('Concurrency: the _flushing reentrancy guard', () => {
  it('a second flushOutbox call started while the first is still awaiting recordEvent does not send again', async () => {
    serverRepliesOnce(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'only' }));

    const resolveSend = holdNextRequest();

    const callsBeforeConcurrentFlushes = sentEntries().length;
    const first = flushOutbox();
    const second = flushOutbox();
    await settle();

    expect(sentEntries().length).toBe(callsBeforeConcurrentFlushes + 1);

    resolveSend();
    await first;
    await second;

    expect(sentEntries().length).toBe(callsBeforeConcurrentFlushes + 1);
    expect(lastPersisted()).toEqual([]);
  });
});

describe('Concurrency: commit-after-send ordering', () => {
  it('the entry leaves the queue only after its send resolves, never while the send is still pending', async () => {
    serverRepliesOnce(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'only' }));

    const resolveSend = holdNextRequest();
    const pending = flushOutbox();
    await settle();

    expect(lastPersisted().map((e) => e.search_id)).toEqual(['only']);

    resolveSend();
    await pending;

    expect(lastPersisted()).toEqual([]);
  });
});

describe('Idempotence: replaying the same event_id', () => {
  it('apply(apply(enqueueCritical)) equals apply(enqueueCritical) when the mint collides on the same event_id', async () => {
    const mintSpy = jest
      .spyOn(Crypto, 'randomUUID')
      .mockReturnValue('22222222-2222-4222-8222-222222222222');
    serverReplies(HTTP_UNAVAILABLE);

    await enqueueCritical(event({ type: 'wrong_album', search_id: 'replay' }));
    const afterOnce = lastPersisted();

    await enqueueCritical(event({ type: 'wrong_album', search_id: 'replay' }));
    const afterTwice = lastPersisted();
    mintSpy.mockRestore();

    expect(afterOnce).toHaveLength(1);
    expect(afterTwice).toHaveLength(1);
    expect(afterTwice?.[0]?.event_id).toBe(afterOnce?.[0]?.event_id);
  });
});

describe('Idempotence: flushing an already-drained queue', () => {
  it('is a no-op, identical to the drain that already happened', async () => {
    await enqueueCritical(event({ search_id: 'drains' }));
    const recordCallsAfterDrain = sentEntries().length;

    await flushOutbox();

    expect(sentEntries().length).toBe(recordCallsAfterDrain);
    expect(lastPersisted()).toEqual([]);
  });
});

interface FreshApp {
  enqueueCritical: typeof enqueueCritical;
  loadPersistedOutbox: typeof loadPersistedOutbox;
  listeners: AppStateChangeHandler[];
}

function bootFreshApp(): FreshApp {
  let app!: FreshApp;
  jest.isolateModules(() => {
    const signIn = require('@testing/signInTestUser') as { signInTestUser: () => Promise<void> };
    void signIn.signInTestUser();
    const appState = require('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    };
    app = {
      enqueueCritical: (
        require('@shared/telemetry/outbox') as { enqueueCritical: typeof enqueueCritical }
      ).enqueueCritical,
      loadPersistedOutbox: (
        require('@shared/telemetry/outboxStore') as {
          loadPersistedOutbox: typeof loadPersistedOutbox;
        }
      ).loadPersistedOutbox,
      listeners: appState.__listeners,
    };
  });
  return app;
}

describe('Idempotence: a foreground transition arriving twice', () => {
  it('flushes on the first active transition and is a no-op on the second, back-to-back', async () => {
    const app = bootFreshApp();
    serverReplies(HTTP_UNAVAILABLE);

    await app.enqueueCritical(event({ search_id: 'q' }));
    expect(sentEntries()).toHaveLength(1);

    const realNow = Date.now();
    const clock = jest.spyOn(Date, 'now').mockReturnValue(realNow + FLUSH_BACKOFF_CAP_MS);
    serverReplies(HTTP_ACCEPTED);
    app.listeners.forEach((handler) => handler('active'));
    await settle();

    expect(sentEntries()).toHaveLength(SECOND_ATTEMPT);
    expect(app.loadPersistedOutbox()).toEqual([]);

    app.listeners.forEach((handler) => handler('active'));
    await settle();

    expect(sentEntries()).toHaveLength(SECOND_ATTEMPT);
    clock.mockRestore();
  });

  it('does not flush on a transition to background, inactive, or any status other than active', async () => {
    const app = bootFreshApp();
    serverReplies(HTTP_UNAVAILABLE);

    await app.enqueueCritical(event({ search_id: 'q' }));
    const callsAfterEnqueue = sentEntries().length;

    for (const status of ['background', 'inactive', 'extension', 'unknown']) {
      app.listeners.forEach((handler) => handler(status));
      await settle();
    }

    expect(sentEntries().length).toBe(callsAfterEnqueue);
  });
});

describe('Regression: repeated enqueueCritical calls must not accumulate AppState listeners', () => {
  it('registers exactly one AppState listener no matter how many times enqueueCritical runs', async () => {
    const app = bootFreshApp();
    serverReplies(HTTP_UNAVAILABLE);

    await app.enqueueCritical(event({ search_id: 'first' }));
    const listenerCountAfterFirst = app.listeners.length;
    await app.enqueueCritical(event({ search_id: 'second' }));
    await app.enqueueCritical(event({ search_id: 'third' }));

    expect(app.listeners.length).toBe(listenerCountAfterFirst);
  });
});

describe('Failure injection: recordEvent is the one I/O call site', () => {
  it('rejecting on every entry attempts each one once and leaves the whole queue, in order, on disk', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    received.length = 0;

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b']);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });

  it('rejecting on a middle entry commits everything sent around it and leaves only the failure queued', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    await enqueueCritical(event({ search_id: 'c' }));
    resetServer();
    serverRepliesOnce(HTTP_ACCEPTED);
    serverRepliesOnce(HTTP_UNAVAILABLE);

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b', 'c']);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['b']);
  });

  it('rejecting on every entry across repeated flush calls never drains the queue and always retries from the front', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    received.length = 0;

    await flushOutbox();
    await flushOutbox();
    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b', 'a', 'b', 'a', 'b']);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });
});

describe('Regression: a permanently rejected entry must not block the queue behind it', () => {
  it('drops an entry the server rejects with 400 and keeps draining the rest', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'poisoned' }));
    await enqueueCritical(event({ search_id: 'b' }));
    await enqueueCritical(event({ search_id: 'c' }));

    resetServer();
    serverReplies((entry) => (entry.search_id === 'poisoned' ? HTTP_BAD_REQUEST : HTTP_ACCEPTED));

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['poisoned', 'b', 'c']);
    expect(lastPersisted()).toEqual([]);
  });

  it('still stops at the first retryable failure, so a transient outage never discards an entry', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));

    resetServer();
    serverReplies('unreachable');

    await flushOutbox();

    expect(sentEntries()).toHaveLength(1);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });

  it('retries a 401 rather than dropping it, so a briefly absent session never loses a label', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));

    resetServer();
    serverReplies(HTTP_UNAUTHORIZED);

    await flushOutbox();

    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a']);
  });
});

describe('Regression: every non-transient 4xx is a permanent rejection', () => {
  it.each([403, 404, 413, 422])('drops an entry the server rejects with %i', async (status) => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));

    resetServer();
    serverReplies(status);

    await flushOutbox();

    expect(lastPersisted()).toEqual([]);
  });

  it.each([401, 408, 429, 500, 503])(
    'retries an entry whose send fails with %i',
    async (status) => {
      serverReplies(HTTP_UNAVAILABLE);
      await enqueueCritical(event({ search_id: 'a' }));

      resetServer();
      serverReplies(status);

      await flushOutbox();

      expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a']);
    },
  );
});

describe('Security: entries are owned by the user who queued them', () => {
  it('tags an entry with the current owner on disk but strips the tag from what is sent', async () => {
    setOutboxOwner('user-a');
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'mine' }));

    expect(lastPersisted()?.map((e) => e.owner_user_id)).toEqual(['user-a']);
    expect(sentEntries()[0]).not.toHaveProperty('owner_user_id');
  });

  it("switching the owner drops the previous user's entries and keeps the new user's", async () => {
    restoreFromDisk([
      { type: 'library_add', event_id: 'a', client_occurred_at: 't', owner_user_id: 'user-a' },
      { type: 'library_add', event_id: 'b', client_occurred_at: 't', owner_user_id: 'user-b' },
      { type: 'library_add', event_id: 'untagged', client_occurred_at: 't' },
    ]);

    setOutboxOwner('user-b');
    expect(lastPersisted()?.map((e) => e.event_id)).toEqual(['b']);

    await flushOutbox();

    expect(sentEntries().map((e) => e.event_id)).toEqual(['b']);
  });

  it('with nobody signed in, a flush never sends an entry owned by a user', async () => {
    restoreFromDisk([
      { type: 'library_add', event_id: 'a', client_occurred_at: 't', owner_user_id: 'user-a' },
    ]);

    await flushOutbox();

    expect(sentEntries()).toHaveLength(0);
    expect(lastPersisted().map((e) => e.event_id)).toEqual(['a']);
  });

  it('an in-flight flush stops sending once the queue is cleared for an account switch', async () => {
    setOutboxOwner('user-a');
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a1' }));
    await enqueueCritical(event({ search_id: 'a2' }));

    resetServer();
    const release = holdNextRequest();
    const flushing = flushOutbox();

    clearOutbox();
    setOutboxOwner('user-b');
    release();
    await flushing;

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a1']);
    expect(lastPersisted()).toEqual([]);
  });
});

function persisted(eventId: string, overrides: Partial<OutboxEntry> = {}): OutboxEntry {
  return { type: 'library_add', event_id: eventId, client_occurred_at: 't', ...overrides };
}

function failFor(eventId: string, reply: Reply): void {
  serverReplies((entry) => (entry.event_id === eventId ? reply : HTTP_ACCEPTED));
}

describe('Regression: a persistently failing entry never starves the entries queued behind it', () => {
  it.each([
    ['a 5xx', HTTP_UNAVAILABLE],
    ['an expired-token HTTP_UNAUTHORIZED', HTTP_UNAUTHORIZED],
    ['a 429', HTTP_TOO_MANY_REQUESTS],
    ['a HTTP_SERVER_ERROR', HTTP_SERVER_ERROR],
  ] as const)(
    'attempts and delivers every later entry while %s keeps the head entry queued',
    async (_label, status) => {
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      restoreFromDisk([persisted('stuck'), persisted('b'), persisted('c')]);
      failFor('stuck', status);

      await flushOutbox();

      expect(sentEntries().map((e) => e.event_id)).toEqual(['stuck', 'b', 'c']);
      expect(lastPersisted()?.map((e) => e.event_id)).toEqual(['stuck']);
      warn.mockRestore();
    },
  );

  it('logs the failing entry type, event_id and error before moving on', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const error = expect.objectContaining({ status: 503 });
    restoreFromDisk([persisted('stuck', { type: 'wrong_album' }), persisted('b')]);
    failFor('stuck', HTTP_UNAVAILABLE);

    await flushOutbox();

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('wrong_album stuck'), error);
    warn.mockRestore();
  });

  it('logs an entry dropped as permanently rejected too, so a chronic 400 is not silent', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const error = expect.objectContaining({ status: 400 });
    restoreFromDisk([persisted('poisoned')]);
    failFor('poisoned', HTTP_BAD_REQUEST);

    await flushOutbox();

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('library_add poisoned'), error);
    expect(lastPersisted()).toEqual([]);
    warn.mockRestore();
  });

  it('moving past a failed entry still never sends an entry owned by another user', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    restoreFromDisk([
      persisted('stuck'),
      persisted('user-a-entry', { owner_user_id: 'user-a' }),
      persisted('ok'),
    ]);
    failFor('stuck', HTTP_UNAVAILABLE);

    await flushOutbox();

    expect(sentEntries().map((e) => e.event_id)).toEqual(['stuck', 'ok']);
    expect(lastPersisted()?.map((e) => e.event_id)).toEqual(['stuck', 'user-a-entry']);
    warn.mockRestore();
  });

  it('surfaces the running droppedCriticalCount in the failed-pass log so a sustained drop rate is visible', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    restoreFromDisk(Array.from({ length: MAX_ENTRIES + OVERFLOW }, (_, i) => persisted(`q${i}`)));
    serverReplies('unreachable');

    await flushOutbox();

    expect(droppedCriticalCount()).toBe(2);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('2 dropped at cap'));
    warn.mockRestore();
  });
});

describe('Backoff: the flush loop retries on its own capped, jittered schedule', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  function foreground(): void {
    const appState = require('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    };
    appState.__listeners.forEach((handler) => handler('active'));
  }

  it('after a failed pass, neither a new enqueue nor a foreground transition retries before the backoff elapses', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    expect(sentEntries()).toHaveLength(1);
    received.length = 0;

    await enqueueCritical(event({ search_id: 'b' }));
    foreground();
    await settle();

    expect(sentEntries()).toHaveLength(0);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });

  it('retries by itself once the backoff elapses, with no enqueue or foreground trigger', async () => {
    serverRepliesOnce(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    received.length = 0;

    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_BASE_MS);

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a']);
    expect(lastPersisted()).toEqual([]);
  });

  it('doubles the wait on each consecutive failed pass and resets it after a clean pass', async () => {
    const random = jest.spyOn(Math, 'random').mockReturnValue(0);
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    expect(sentEntries()).toHaveLength(1);

    for (const [attempt, wait] of [
      [2, FLUSH_BACKOFF_BASE_MS / 2],
      [3, FLUSH_BACKOFF_BASE_MS],
    ] as const) {
      await jest.advanceTimersByTimeAsync(wait - 1);
      expect(sentEntries()).toHaveLength(attempt - 1);
      await jest.advanceTimersByTimeAsync(1);
      expect(sentEntries()).toHaveLength(attempt);
    }

    serverReplies(HTTP_ACCEPTED);
    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_BASE_MS * 2);
    expect(sentEntries()).toHaveLength(FOURTH_ATTEMPT);
    expect(lastPersisted()).toEqual([]);

    received.length = 0;
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'after-recovery' }));
    expect(sentEntries()).toHaveLength(1);
    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_BASE_MS / 2);
    expect(sentEntries()).toHaveLength(SECOND_ATTEMPT);
    random.mockRestore();
  });

  it('clearOutbox cancels the pending retry, so the next account neither inherits the wait nor a stale timer', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'user-a' }));

    clearOutbox();
    resetServer();
    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_CAP_MS);
    expect(sentEntries()).toHaveLength(0);

    await enqueueCritical(event({ search_id: 'user-b' }));
    expect(sentEntries().map((e) => e.search_id)).toEqual(['user-b']);
  });

  it('a retry timer that fires into an already-drained queue sends nothing', async () => {
    serverRepliesOnce(HTTP_UNAVAILABLE);
    await enqueueCritical(event({ search_id: 'a' }));
    await flushOutbox();
    received.length = 0;

    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_CAP_MS);

    expect(sentEntries()).toHaveLength(0);
  });
});

describe('law: flushBackoffMs is exponential, jittered into [ceiling/2, ceiling], and capped', () => {
  it('starts at the base ceiling and doubles per failed pass', () => {
    expect(flushBackoffMs(1, 0)).toBe(FLUSH_BACKOFF_BASE_MS / 2);
    expect(flushBackoffMs(1, 1)).toBe(FLUSH_BACKOFF_BASE_MS);
    expect(flushBackoffMs(3, 1)).toBe(FLUSH_BACKOFF_BASE_MS * 4);
  });

  it('treats a non-positive pass count as the first pass', () => {
    expect(flushBackoffMs(0, 1)).toBe(FLUSH_BACKOFF_BASE_MS);
  });

  it('never exceeds the cap however many passes have failed', () => {
    for (const passes of [20, 31, 1000]) {
      expect(flushBackoffMs(passes, 0)).toBe(FLUSH_BACKOFF_CAP_MS / 2);
      expect(flushBackoffMs(passes, 0.999999)).toBeLessThanOrEqual(FLUSH_BACKOFF_CAP_MS);
    }
  });
});

describe('pure helpers', () => {
  function entry(eventId: string, overrides: Partial<OutboxEntry> = {}): OutboxEntry {
    return {
      type: 'play',
      event_id: eventId,
      client_occurred_at: '2026-01-01T00:00:00.000Z',
      ...overrides,
    };
  }

  describe('capEntries at the length<=max boundary', () => {
    it.each<[string, number, number]>([
      ['one below max keeps every entry', 3, 2],
      ['exactly at max keeps every entry', 3, 3],
      ['one above max drops only the oldest entry', 3, 4],
    ])('%s', (_label, max, length) => {
      const xs = Array.from({ length }, (_, i) => entry(`id-${i}`));
      const result = capEntries(xs, max);

      if (length <= max) {
        expect(result).toEqual(xs);
        expect(result).not.toBe(xs);
        return;
      }

      expect(result.map((e) => e.event_id)).toEqual(['id-1', 'id-2', 'id-3']);
    });
  });

  describe('capEntries with max === 0', () => {
    it('leaves an already-empty queue empty', () => {
      expect(capEntries([], 0)).toEqual([]);
    });

    it('drops every entry regardless of how many there are', () => {
      const xs = [entry('a'), entry('b'), entry('c')];
      expect(capEntries(xs, 0)).toEqual([]);
    });
  });

  describe('dedupeById', () => {
    it('returns an empty array for an empty queue', () => {
      expect(dedupeById([])).toEqual([]);
    });

    it('keeps every entry when all event_ids are distinct', () => {
      const xs = [entry('a'), entry('b'), entry('c')];
      expect(dedupeById(xs)).toEqual(xs);
    });

    it('on a plain collision, the later entry wins', () => {
      const older = entry('x');
      const newer = entry('x');
      expect(dedupeById([older, newer])).toEqual([newer]);
    });

    it('on a collision where the later entry carries different data, the later data wins at the earlier position', () => {
      const first = entry('x', { payload: { attempt: 1 } });
      const middle = entry('y');
      const last = entry('x', { payload: { attempt: 2 } });

      const result = dedupeById([first, middle, last]);

      expect(result).toEqual([last, middle]);
    });
  });

  describe('withEnvelope', () => {
    it('stamps a bare event with the given id and timestamp', () => {
      const event: DiscoveryEvent = { type: 'play' };

      const result = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

      expect(result).toEqual({
        type: 'play',
        event_id: 'id-1',
        client_occurred_at: '2026-01-01T00:00:00.000Z',
      });
    });

    it('overrides an event_id and client_occurred_at the event already carries', () => {
      const event: DiscoveryEvent = {
        type: 'play',
        event_id: 'stale-id',
        client_occurred_at: '2020-01-01T00:00:00.000Z',
      };

      const result = withEnvelope(event, 'fresh-id', '2026-01-01T00:00:00.000Z');

      expect(result.event_id).toBe('fresh-id');
      expect(result.client_occurred_at).toBe('2026-01-01T00:00:00.000Z');
    });

    it('preserves the rest of the event unchanged', () => {
      const event: DiscoveryEvent = {
        type: 'result_clicked',
        search_id: 'q',
        payload: { rank: 2 },
      };

      const result = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

      expect(result.search_id).toBe('q');
      expect(result.payload).toEqual({ rank: 2 });
    });

    it('re-enveloping an already-enveloped entry with the same id and timestamp is a no-op', () => {
      const event: DiscoveryEvent = { type: 'play' };
      const once = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

      const twice = withEnvelope(once, 'id-1', '2026-01-01T00:00:00.000Z');

      expect(twice).toEqual(once);
    });
  });
});

describe('capOutbox keeps diagnostics from evicting label-critical entries', () => {
  const queued = (eventId: string, type: OutboxEntry['type']): OutboxEntry => ({
    type,
    event_id: eventId,
    client_occurred_at: '2026-01-01T00:00:00.000Z',
  });
  const label = (id: string) => queued(id, 'library_add');
  const diagnostic = (id: string) => queued(id, 'client_error');

  it('drops the oldest diagnostics first, however many arrive, and never a label', () => {
    const xs = [
      label('l-0'),
      ...Array.from({ length: 10 }, (_, i) => diagnostic(`d-${i}`)),
      label('l-1'),
    ];

    expect(capOutbox(xs, 5, 3).map((e) => e.event_id)).toEqual(['l-0', 'd-7', 'd-8', 'd-9', 'l-1']);
  });

  it('gives diagnostics only the room labels leave under the overall cap', () => {
    const xs = [diagnostic('d-0'), label('l-0'), label('l-1'), label('l-2')];

    expect(capOutbox(xs, 3, 3).map((e) => e.event_id)).toEqual(['l-0', 'l-1', 'l-2']);
  });

  it('keeps every entry, in order, when both caps have room', () => {
    const xs = [label('l-0'), diagnostic('d-0'), label('l-1')];

    expect(capOutbox(xs, 5, 2)).toEqual(xs);
  });
});

describe('outbox_flush_failed telemetry', () => {
  beforeEach(() => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  function flushFailures(): DiscoveryEvent[] {
    return received.filter((e) => e.type === 'outbox_flush_failed');
  }

  it('sends outbox_flush_failed directly, once per failed pass, without queueing it', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical({ type: 'library_add', search_id: 'a' });
    await settle();

    expect(flushFailures()).toEqual([
      {
        type: 'outbox_flush_failed',
        payload: expect.objectContaining({ failed_passes: 1, queued: 1, dropped_at_cap: 0 }),
      },
    ]);
    expect(lastPersisted()?.map((e) => e.type)).toEqual(['library_add']);
  });

  it('counts each further failed pass and survives a rejected telemetry send', async () => {
    serverReplies(HTTP_UNAVAILABLE);
    await enqueueCritical({ type: 'library_add', search_id: 'a' });

    await jest.advanceTimersByTimeAsync(60_000);

    const passes = flushFailures().map((e) => e.payload?.['failed_passes']);
    expect(passes.slice(0, 2)).toEqual([1, 2]);
  });
});
