import * as Crypto from 'expo-crypto';

import { ApiError, NetworkError } from '@shared/api-client';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import type { OutboxEntry } from '../outbox';
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
  dedupeById,
  withEnvelope,
} from '../outbox';
import { loadPersistedOutbox, persistOutbox } from '../outboxStore';
import { recordEvent, type DiscoveryEvent } from '../recordEvent';
import type * as RecordEventModule from '../recordEvent';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

type AppStateChangeHandler = (state: string) => void;

jest.mock('../outboxStore', () => ({
  loadPersistedOutbox: jest.fn(),
  persistOutbox: jest.fn(),
}));

jest.mock('../recordEvent', () => ({ recordEvent: jest.fn() }));

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

const loadPersistedOutboxMock = loadPersistedOutbox as jest.MockedFunction<
  typeof loadPersistedOutbox
>;
const persistOutboxMock = persistOutbox as jest.MockedFunction<typeof persistOutbox>;
const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;

const MAX_ENTRIES = 50;

function event(overrides: Partial<DiscoveryEvent> = {}): DiscoveryEvent {
  return { type: 'library_add', ...overrides };
}

function sentEntries(): OutboxEntry[] {
  return recordEventMock.mock.calls.map(([entry]) => entry as OutboxEntry);
}

function lastPersisted(): readonly OutboxEntry[] | undefined {
  return persistOutboxMock.mock.calls.at(-1)?.[0];
}

beforeEach(() => {
  // A failed pass arms the outbox's retry timer. Fake timers keep every module
  // instance's timer (including the isolateModules ones) from firing into a later
  // test; the backoff tests advance them explicitly.
  jest.useFakeTimers();
  _resetOutboxForTest();
  loadPersistedOutboxMock.mockReset().mockReturnValue([]);
  persistOutboxMock.mockReset();
  recordEventMock.mockReset().mockResolvedValue(undefined);
});

afterEach(() => {
  _resetOutboxForTest();
  jest.useRealTimers();
});

// Kept ahead of the tests below: the isolated outbox instances they boot stay subscribed
// to the AppState mock, and a foreground event fired here would flush their queues too.
describe('remote kill switch', () => {
  // Regression for issue #955: the telemetry outbox must not send while its remote kill switch is
  // off, so repeated failing POSTs can be stopped without an app release.
  type AppStateChangeHandler = (state: string) => void;

  const { __listeners: appStateListeners } = jest.requireMock(
    'react-native/Libraries/AppState/AppState',
  ) as { __listeners: AppStateChangeHandler[] };

  function queuedIds(): (string | undefined)[] {
    return (persistOutboxMock.mock.calls.at(-1)?.[0] ?? []).map((e) => e.search_id);
  }

  async function settle(): Promise<void> {
    for (let i = 0; i < 20; i += 1) await Promise.resolve();
  }

  beforeEach(() => {
    jest.useFakeTimers();
    setKillSwitchFileStore(createMemoryFileStore());
    _resetOutboxForTest();
    (loadPersistedOutbox as jest.Mock).mockReset().mockReturnValue([]);
    persistOutboxMock.mockReset();
    recordEventMock.mockReset().mockResolvedValue(undefined);
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

      expect(recordEventMock).not.toHaveBeenCalled();
      expect(queuedIds()).toEqual(['a']);
    });

    it('arms no retry timer while the switch is off', async () => {
      recordEventMock.mockRejectedValue(new Error('server down'));
      await enqueueCritical({ type: 'library_add', search_id: 'a' });
      expect(recordEventMock).toHaveBeenCalledTimes(1);

      applyKillSwitches({ telemetry_enabled: false });
      await jest.advanceTimersByTimeAsync(10 * 60 * 1000);

      expect(recordEventMock).toHaveBeenCalledTimes(1);
      expect(jest.getTimerCount()).toBe(0);
    });

    it('stops a running pass before its next entry when the switch is turned off', async () => {
      let releaseFirst!: () => void;
      recordEventMock.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            releaseFirst = resolve;
          }),
      );
      _resetOutboxForTest({ restored: false });
      (loadPersistedOutbox as jest.Mock).mockReturnValue(
        ['a', 'b'].map((id) => ({
          type: 'library_add',
          search_id: id,
          event_id: `evt-${id}`,
          client_occurred_at: '2026-09-15T00:00:00.000Z',
        })),
      );
      const pass = flushOutbox();
      await settle();
      expect(recordEventMock).toHaveBeenCalledTimes(1);

      applyKillSwitches({ telemetry_enabled: false });
      releaseFirst();
      await pass;

      expect(recordEventMock.mock.calls.map(([e]) => e.search_id)).toEqual(['a']);
      expect(queuedIds()).toEqual(['b']);
    });

    it('flushes what was held back as soon as the switch is turned back on', async () => {
      applyKillSwitches({ telemetry_enabled: false });
      await enqueueCritical({ type: 'library_add', search_id: 'a' });

      applyKillSwitches({ telemetry_enabled: true });
      await settle();

      expect(recordEventMock.mock.calls.map(([e]) => e.search_id)).toEqual(['a']);
      expect(queuedIds()).toEqual([]);
    });
  });
});

describe('Reducer: enqueueCritical', () => {
  it('enqueuing onto an empty queue commits the new entry to disk before its send has resolved', () => {
    recordEventMock.mockReturnValue(new Promise(() => {}));

    enqueueCritical(event({ search_id: 'first' })).catch(() => undefined);

    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['first']);
  });

  it('enqueuing past MAX_ENTRIES caps the queue at MAX_ENTRIES, dropping the oldest entry', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

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
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

    await enqueueCritical(event({ search_id: 'first' }));
    await enqueueCritical(event({ search_id: 'second' }));
    mintSpy.mockRestore();

    const persisted = lastPersisted();
    expect(persisted).toHaveLength(1);
    expect(persisted?.[0]?.search_id).toBe('second');
  });
});

describe('Backpressure: shedding a label-critical entry at the cap is recorded, never silent', () => {
  it('records the drop when enqueuing past MAX_ENTRIES sheds the oldest label-critical entry', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
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
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

    for (let i = 0; i < MAX_ENTRIES; i += 1) {
      await enqueueCritical(event({ search_id: `q${i}` }));
    }

    expect(droppedCriticalCount()).toBe(0);
  });

  it('records a plural drop when a restored outbox exceeds the cap by more than one', () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    let dropped: number | undefined;

    jest.isolateModules(() => {
      const outboxStoreModule = require('../outboxStore') as {
        loadPersistedOutbox: jest.MockedFunction<typeof loadPersistedOutbox>;
        persistOutbox: jest.MockedFunction<typeof persistOutbox>;
      };
      const recordEventModule = require('../recordEvent') as {
        recordEvent: jest.MockedFunction<typeof recordEvent>;
      };
      const overfull: OutboxEntry[] = Array.from({ length: MAX_ENTRIES + 2 }, (_, i) => ({
        ...event({ search_id: `q${i}` }),
        event_id: `persisted-${i}`,
        client_occurred_at: '2026-01-01T00:00:00.000Z',
      }));
      outboxStoreModule.loadPersistedOutbox.mockReturnValue(overfull);
      recordEventModule.recordEvent.mockRejectedValue(new Error('send unavailable'));

      const outbox = require('../outbox') as {
        flushOutbox: typeof flushOutbox;
        droppedCriticalCount: typeof droppedCriticalCount;
      };
      // ensureRestored fires synchronously inside flushOutbox, capping the 52
      // restored entries to 50 and shedding 2 in a single call — the plural path.
      void outbox.flushOutbox();
      dropped = outbox.droppedCriticalCount();
    });

    expect(dropped).toBe(2);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('entries at cap'));

    warn.mockRestore();
  });
});

describe('Security: clearOutbox drops queued telemetry on sign-out / account switch', () => {
  it('empties the in-memory queue and deletes the persisted outbox on disk', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'user-a-report' }));
    expect(lastPersisted()).toHaveLength(1);

    clearOutbox();

    expect(lastPersisted()).toEqual([]);
  });

  it("a flush after clear sends nothing, so user A's queued entry cannot be delivered under user B's session", async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'user-a-report' }));

    clearOutbox();
    recordEventMock.mockReset().mockResolvedValue(undefined);
    await flushOutbox();

    expect(recordEventMock).not.toHaveBeenCalled();
  });

  it('runs off the sign-out registry, so no caller in auth has to reach for it', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'user-a-report' }));

    runSignOutCleanups();

    expect(lastPersisted()).toEqual([]);
  });
});

describe('Reducer: flushOutbox', () => {
  it('flushing an empty queue sends nothing and writes nothing to disk', async () => {
    await flushOutbox();

    expect(recordEventMock).not.toHaveBeenCalled();
    expect(persistOutboxMock).not.toHaveBeenCalled();
  });

  it('a full drain sends every queued entry once, in order, and ends with an empty queue on disk', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    recordEventMock.mockReset().mockResolvedValue(undefined);

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b']);
    expect(lastPersisted()).toEqual([]);
  });
});

describe('Reducer: ensureRestored (cold start, driven through a fresh module instance)', () => {
  it('merges the persisted outbox into memory on first use and dedupes a corrupted duplicate event_id, keeping the later record', async () => {
    let freshFlushOutbox!: typeof flushOutbox;
    let freshRecordEvent!: jest.MockedFunction<typeof recordEvent>;
    let freshPersistOutbox!: jest.MockedFunction<typeof persistOutbox>;

    jest.isolateModules(() => {
      const outboxStoreModule = require('../outboxStore') as {
        loadPersistedOutbox: jest.MockedFunction<typeof loadPersistedOutbox>;
        persistOutbox: jest.MockedFunction<typeof persistOutbox>;
      };
      const recordEventModule = require('../recordEvent') as {
        recordEvent: jest.MockedFunction<typeof recordEvent>;
      };
      outboxStoreModule.loadPersistedOutbox.mockReturnValue([
        { type: 'library_add', event_id: 'disk-1', client_occurred_at: 'stale' },
        { type: 'library_add', event_id: 'disk-1', client_occurred_at: 'fresh' },
        { type: 'wrong_album', event_id: 'disk-2', client_occurred_at: 't2' },
      ]);
      recordEventModule.recordEvent.mockResolvedValue(undefined);
      freshPersistOutbox = outboxStoreModule.persistOutbox;
      freshRecordEvent = recordEventModule.recordEvent;
      freshFlushOutbox = (require('../outbox') as { flushOutbox: typeof flushOutbox }).flushOutbox;
    });

    await freshFlushOutbox();

    expect(freshRecordEvent.mock.calls.map(([entry]) => (entry as OutboxEntry).event_id)).toEqual([
      'disk-1',
      'disk-2',
    ]);
    expect((freshRecordEvent.mock.calls[0]?.[0] as OutboxEntry).client_occurred_at).toBe('fresh');
    expect(freshPersistOutbox.mock.calls.at(-1)?.[0]).toEqual([]);
  });
});

describe('Concurrency: the _flushing reentrancy guard', () => {
  it('a second flushOutbox call started while the first is still awaiting recordEvent does not send again', async () => {
    recordEventMock.mockRejectedValueOnce(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'only' }));

    let resolveSend!: () => void;
    recordEventMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveSend = resolve;
        }),
    );

    const callsBeforeConcurrentFlushes = recordEventMock.mock.calls.length;
    const first = flushOutbox();
    const second = flushOutbox();
    await Promise.resolve();
    await Promise.resolve();

    expect(recordEventMock.mock.calls.length).toBe(callsBeforeConcurrentFlushes + 1);

    resolveSend();
    await first;
    await second;

    expect(recordEventMock.mock.calls.length).toBe(callsBeforeConcurrentFlushes + 1);
    expect(lastPersisted()).toEqual([]);
  });
});

describe('Concurrency: commit-after-send ordering', () => {
  it('the entry leaves the queue only after its send resolves, never while the send is still pending', async () => {
    recordEventMock.mockRejectedValueOnce(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'only' }));
    const persistCallsBeforeRetry = persistOutboxMock.mock.calls.length;

    let resolveSend!: () => void;
    recordEventMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveSend = resolve;
        }),
    );
    const pending = flushOutbox();
    await Promise.resolve();
    await Promise.resolve();

    expect(persistOutboxMock.mock.calls.length).toBe(persistCallsBeforeRetry);

    resolveSend();
    await pending;

    expect(persistOutboxMock.mock.calls.length).toBe(persistCallsBeforeRetry + 1);
    expect(lastPersisted()).toEqual([]);
  });
});

describe('Idempotence: replaying the same event_id', () => {
  it('apply(apply(enqueueCritical)) equals apply(enqueueCritical) when the mint collides on the same event_id', async () => {
    const mintSpy = jest
      .spyOn(Crypto, 'randomUUID')
      .mockReturnValue('22222222-2222-4222-8222-222222222222');
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

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
    const recordCallsAfterDrain = recordEventMock.mock.calls.length;
    const persistCallsAfterDrain = persistOutboxMock.mock.calls.length;

    await flushOutbox();

    expect(recordEventMock.mock.calls.length).toBe(recordCallsAfterDrain);
    expect(persistOutboxMock.mock.calls.length).toBe(persistCallsAfterDrain);
  });
});

describe('Idempotence: a foreground transition arriving twice', () => {
  it('flushes on the first active transition and is a no-op on the second, back-to-back', async () => {
    let listeners!: AppStateChangeHandler[];
    let freshEnqueue!: typeof enqueueCritical;
    let freshRecordEvent!: jest.MockedFunction<typeof recordEvent>;
    let freshPersistOutbox!: jest.MockedFunction<typeof persistOutbox>;

    jest.isolateModules(() => {
      const outboxStoreModule = require('../outboxStore') as {
        loadPersistedOutbox: jest.MockedFunction<typeof loadPersistedOutbox>;
        persistOutbox: jest.MockedFunction<typeof persistOutbox>;
      };
      const recordEventModule = require('../recordEvent') as {
        recordEvent: jest.MockedFunction<typeof recordEvent>;
      };
      const appStateModule = require('react-native/Libraries/AppState/AppState') as {
        __listeners: AppStateChangeHandler[];
      };
      outboxStoreModule.loadPersistedOutbox.mockReturnValue([]);
      recordEventModule.recordEvent.mockRejectedValue(new Error('send unavailable'));
      listeners = appStateModule.__listeners;
      freshPersistOutbox = outboxStoreModule.persistOutbox;
      freshRecordEvent = recordEventModule.recordEvent;
      freshEnqueue = (require('../outbox') as { enqueueCritical: typeof enqueueCritical })
        .enqueueCritical;
    });

    await freshEnqueue(event({ search_id: 'q' }));
    expect(freshRecordEvent).toHaveBeenCalledTimes(1);

    // The failed send armed the flush backoff; the user returns after it elapsed.
    const realNow = Date.now();
    const clock = jest.spyOn(Date, 'now').mockReturnValue(realNow + FLUSH_BACKOFF_CAP_MS);
    freshRecordEvent.mockResolvedValue(undefined);
    listeners.forEach((handler) => handler('active'));
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    expect(freshRecordEvent).toHaveBeenCalledTimes(2);
    expect(freshPersistOutbox.mock.calls.at(-1)?.[0]).toEqual([]);

    listeners.forEach((handler) => handler('active'));
    await Promise.resolve();
    await Promise.resolve();

    expect(freshRecordEvent).toHaveBeenCalledTimes(2);
    clock.mockRestore();
  });

  it('does not flush on a transition to background, inactive, or any status other than active', async () => {
    let listeners!: AppStateChangeHandler[];
    let freshEnqueue!: typeof enqueueCritical;
    let freshRecordEvent!: jest.MockedFunction<typeof recordEvent>;

    jest.isolateModules(() => {
      const outboxStoreModule = require('../outboxStore') as {
        loadPersistedOutbox: jest.MockedFunction<typeof loadPersistedOutbox>;
        persistOutbox: jest.MockedFunction<typeof persistOutbox>;
      };
      const recordEventModule = require('../recordEvent') as {
        recordEvent: jest.MockedFunction<typeof recordEvent>;
      };
      const appStateModule = require('react-native/Libraries/AppState/AppState') as {
        __listeners: AppStateChangeHandler[];
      };
      outboxStoreModule.loadPersistedOutbox.mockReturnValue([]);
      recordEventModule.recordEvent.mockRejectedValue(new Error('send unavailable'));
      listeners = appStateModule.__listeners;
      freshRecordEvent = recordEventModule.recordEvent;
      freshEnqueue = (require('../outbox') as { enqueueCritical: typeof enqueueCritical })
        .enqueueCritical;
    });

    await freshEnqueue(event({ search_id: 'q' }));
    const callsAfterEnqueue = freshRecordEvent.mock.calls.length;

    for (const status of ['background', 'inactive', 'extension', 'unknown']) {
      listeners.forEach((handler) => handler(status));
      await Promise.resolve();
      await Promise.resolve();
    }

    expect(freshRecordEvent.mock.calls.length).toBe(callsAfterEnqueue);
  });
});

describe('Regression: repeated enqueueCritical calls must not accumulate AppState listeners', () => {
  it('registers exactly one AppState listener no matter how many times enqueueCritical runs', async () => {
    let listeners!: AppStateChangeHandler[];
    let freshEnqueue!: typeof enqueueCritical;

    jest.isolateModules(() => {
      const outboxStoreModule = require('../outboxStore') as {
        loadPersistedOutbox: jest.MockedFunction<typeof loadPersistedOutbox>;
        persistOutbox: jest.MockedFunction<typeof persistOutbox>;
      };
      const recordEventModule = require('../recordEvent') as {
        recordEvent: jest.MockedFunction<typeof recordEvent>;
      };
      const appStateModule = require('react-native/Libraries/AppState/AppState') as {
        __listeners: AppStateChangeHandler[];
      };
      outboxStoreModule.loadPersistedOutbox.mockReturnValue([]);
      recordEventModule.recordEvent.mockRejectedValue(new Error('send unavailable'));
      listeners = appStateModule.__listeners;
      freshEnqueue = (require('../outbox') as { enqueueCritical: typeof enqueueCritical })
        .enqueueCritical;
    });

    const listenerCountBefore = listeners.length;

    await freshEnqueue(event({ search_id: 'first' }));
    await freshEnqueue(event({ search_id: 'second' }));
    await freshEnqueue(event({ search_id: 'third' }));

    expect(listeners.length - listenerCountBefore).toBe(1);
  });
});

describe('Failure injection: recordEvent is the one I/O call site', () => {
  it('rejecting on every entry attempts each one once and leaves the whole queue, in order, on disk', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    recordEventMock.mockClear();

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b']);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });

  it('rejecting on a middle entry commits everything sent around it and leaves only the failure queued', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    await enqueueCritical(event({ search_id: 'c' }));
    recordEventMock.mockReset();
    recordEventMock.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('still down'));

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b', 'c']);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['b']);
  });

  it('rejecting on every entry across repeated flush calls never drains the queue and always retries from the front', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));
    recordEventMock.mockClear();

    await flushOutbox();
    await flushOutbox();
    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a', 'b', 'a', 'b', 'a', 'b']);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });
});

describe('Regression: a permanently rejected entry must not block the queue behind it', () => {
  it('drops an entry the server rejects with 400 and keeps draining the rest', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'poisoned' }));
    await enqueueCritical(event({ search_id: 'b' }));
    await enqueueCritical(event({ search_id: 'c' }));

    recordEventMock.mockReset();
    recordEventMock.mockImplementation(async (entry: DiscoveryEvent) => {
      if (entry.search_id === 'poisoned')
        throw new ApiError(400, 'payload.result_signature must be a string');
      return undefined;
    });

    await flushOutbox();

    expect(sentEntries().map((e) => e.search_id)).toEqual(['poisoned', 'b', 'c']);
    expect(lastPersisted()).toEqual([]);
  });

  it('still stops at the first retryable failure, so a transient outage never discards an entry', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    await enqueueCritical(event({ search_id: 'b' }));

    recordEventMock.mockReset();
    recordEventMock.mockRejectedValue(new NetworkError('transport', 'unreachable'));

    await flushOutbox();

    expect(recordEventMock).toHaveBeenCalledTimes(1);
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });

  it('retries a 401 rather than dropping it, so a briefly absent session never loses a label', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));

    recordEventMock.mockReset();
    recordEventMock.mockRejectedValue(new ApiError(401, 'no active session'));

    await flushOutbox();

    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a']);
  });
});

// Regression test for #960.
describe('Security: entries are owned by the user who queued them', () => {
  it('tags an entry with the current owner on disk but strips the tag from what is sent', async () => {
    setOutboxOwner('user-a');
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'mine' }));

    expect(lastPersisted()?.map((e) => e.owner_user_id)).toEqual(['user-a']);
    expect(sentEntries()[0]).not.toHaveProperty('owner_user_id');
  });

  it("switching the owner drops the previous user's entries and keeps the new user's", async () => {
    loadPersistedOutboxMock.mockReturnValue([
      { type: 'library_add', event_id: 'a', client_occurred_at: 't', owner_user_id: 'user-a' },
      { type: 'library_add', event_id: 'b', client_occurred_at: 't', owner_user_id: 'user-b' },
      { type: 'library_add', event_id: 'untagged', client_occurred_at: 't' },
    ]);
    _resetOutboxForTest({ restored: false });

    setOutboxOwner('user-b');
    expect(lastPersisted()?.map((e) => e.event_id)).toEqual(['b']);

    await flushOutbox();

    expect(sentEntries().map((e) => e.event_id)).toEqual(['b']);
  });

  it('with nobody signed in, a flush never sends an entry owned by a user', async () => {
    loadPersistedOutboxMock.mockReturnValue([
      { type: 'library_add', event_id: 'a', client_occurred_at: 't', owner_user_id: 'user-a' },
    ]);
    _resetOutboxForTest({ restored: false });

    await flushOutbox();

    expect(recordEventMock).not.toHaveBeenCalled();
    expect(lastPersisted()).toBeUndefined();
  });

  it('an in-flight flush stops sending once the queue is cleared for an account switch', async () => {
    setOutboxOwner('user-a');
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ search_id: 'a1' }));
    await enqueueCritical(event({ search_id: 'a2' }));

    let release!: () => void;
    recordEventMock.mockReset().mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    recordEventMock.mockResolvedValue(undefined);
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

function restoreFromDisk(entries: OutboxEntry[]): void {
  loadPersistedOutboxMock.mockReturnValue(entries);
  _resetOutboxForTest({ restored: false });
}

function failFor(eventId: string, error: unknown): void {
  recordEventMock.mockImplementation(async (entry: DiscoveryEvent) => {
    if (entry.event_id === eventId) throw error;
    return undefined;
  });
}

// Regression test for #948.
describe('Regression: a persistently failing entry never starves the entries queued behind it', () => {
  it.each([
    ['a 5xx', new ApiError(503, 'unavailable')],
    ['an expired-token 401', new ApiError(401, 'jwt expired')],
    ['a 403', new ApiError(403, 'forbidden')],
    ['an unclassified error', new Error('boom')],
  ])('attempts and delivers every later entry while %s keeps the head entry queued', async (_label, error) => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    restoreFromDisk([persisted('stuck'), persisted('b'), persisted('c')]);
    failFor('stuck', error);

    await flushOutbox();

    expect(sentEntries().map((e) => e.event_id)).toEqual(['stuck', 'b', 'c']);
    expect(lastPersisted()?.map((e) => e.event_id)).toEqual(['stuck']);
    warn.mockRestore();
  });

  it('logs the failing entry type, event_id and error before moving on', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const error = new ApiError(503, 'unavailable');
    restoreFromDisk([persisted('stuck', { type: 'wrong_album' }), persisted('b')]);
    failFor('stuck', error);

    await flushOutbox();

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('wrong_album stuck'), error);
    warn.mockRestore();
  });

  it('logs an entry dropped as permanently rejected too, so a chronic 400 is not silent', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const error = new ApiError(400, 'bad payload');
    restoreFromDisk([persisted('poisoned')]);
    failFor('poisoned', error);

    await flushOutbox();

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('library_add poisoned'), error);
    expect(lastPersisted()).toEqual([]);
    warn.mockRestore();
  });

  // Regression test for #960.
  it('moving past a failed entry still never sends an entry owned by another user', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    restoreFromDisk([
      persisted('stuck'),
      persisted('user-a-entry', { owner_user_id: 'user-a' }),
      persisted('ok'),
    ]);
    failFor('stuck', new ApiError(503, 'unavailable'));

    await flushOutbox();

    expect(sentEntries().map((e) => e.event_id)).toEqual(['stuck', 'ok']);
    expect(lastPersisted()?.map((e) => e.event_id)).toEqual(['stuck', 'user-a-entry']);
    warn.mockRestore();
  });

  it('surfaces the running droppedCriticalCount in the failed-pass log so a sustained drop rate is visible', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    restoreFromDisk(Array.from({ length: MAX_ENTRIES + 2 }, (_, i) => persisted(`q${i}`)));
    recordEventMock.mockRejectedValue(new NetworkError('transport', 'offline'));

    await flushOutbox();

    expect(droppedCriticalCount()).toBe(2);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('2 dropped at cap'));
    warn.mockRestore();
  });
});

// Regression test for #948.
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
    recordEventMock.mockRejectedValue(new ApiError(503, 'unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    expect(recordEventMock).toHaveBeenCalledTimes(1);
    recordEventMock.mockClear();

    await enqueueCritical(event({ search_id: 'b' }));
    foreground();
    await Promise.resolve();
    await Promise.resolve();

    expect(recordEventMock).not.toHaveBeenCalled();
    expect(lastPersisted()?.map((e) => e.search_id)).toEqual(['a', 'b']);
  });

  it('retries by itself once the backoff elapses, with no enqueue or foreground trigger', async () => {
    recordEventMock.mockRejectedValueOnce(new ApiError(503, 'unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    recordEventMock.mockClear();

    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_BASE_MS);

    expect(sentEntries().map((e) => e.search_id)).toEqual(['a']);
    expect(lastPersisted()).toEqual([]);
  });

  it('doubles the wait on each consecutive failed pass and resets it after a clean pass', async () => {
    const random = jest.spyOn(Math, 'random').mockReturnValue(0);
    recordEventMock.mockRejectedValue(new ApiError(503, 'unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    expect(recordEventMock).toHaveBeenCalledTimes(1);

    // random 0 => each wait is exactly half its ceiling: 1x, 2x, 4x base / 2.
    for (const [attempt, wait] of [
      [2, FLUSH_BACKOFF_BASE_MS / 2],
      [3, FLUSH_BACKOFF_BASE_MS],
    ] as const) {
      await jest.advanceTimersByTimeAsync(wait - 1);
      expect(recordEventMock).toHaveBeenCalledTimes(attempt - 1);
      await jest.advanceTimersByTimeAsync(1);
      expect(recordEventMock).toHaveBeenCalledTimes(attempt);
    }

    recordEventMock.mockResolvedValue(undefined);
    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_BASE_MS * 2);
    expect(recordEventMock).toHaveBeenCalledTimes(4);
    expect(lastPersisted()).toEqual([]);

    recordEventMock.mockClear().mockRejectedValue(new ApiError(503, 'unavailable'));
    await enqueueCritical(event({ search_id: 'after-recovery' }));
    expect(recordEventMock).toHaveBeenCalledTimes(1);
    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_BASE_MS / 2);
    expect(recordEventMock).toHaveBeenCalledTimes(2);
    random.mockRestore();
  });

  it('clearOutbox cancels the pending retry, so the next account neither inherits the wait nor a stale timer', async () => {
    recordEventMock.mockRejectedValue(new ApiError(503, 'unavailable'));
    await enqueueCritical(event({ search_id: 'user-a' }));

    clearOutbox();
    recordEventMock.mockReset().mockResolvedValue(undefined);
    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_CAP_MS);
    expect(recordEventMock).not.toHaveBeenCalled();

    await enqueueCritical(event({ search_id: 'user-b' }));
    expect(sentEntries().map((e) => e.search_id)).toEqual(['user-b']);
  });

  it('a retry timer that fires into an already-drained queue sends nothing', async () => {
    recordEventMock.mockRejectedValueOnce(new ApiError(503, 'unavailable'));
    await enqueueCritical(event({ search_id: 'a' }));
    // An explicit drain empties the queue while the retry timer is still armed.
    await flushOutbox();
    recordEventMock.mockClear();

    await jest.advanceTimersByTimeAsync(FLUSH_BACKOFF_CAP_MS);

    expect(recordEventMock).not.toHaveBeenCalled();
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

describe('recordEvent gated after the switch check', () => {
  // This file mocks ../recordEvent wholesale; the gated error is the real class.
  const { TelemetryGatedError } = jest.requireActual<typeof RecordEventModule>('../recordEvent');

  function queuedIds(): (string | undefined)[] {
    return (persistOutboxMock.mock.calls.at(-1)?.[0] ?? []).map((e) => e.search_id);
  }

  beforeEach(() => {
    jest.useFakeTimers();
    _resetOutboxForTest();
    (loadPersistedOutbox as jest.Mock).mockReset().mockReturnValue([]);
    persistOutboxMock.mockReset();
    recordEventMock.mockReset().mockRejectedValue(new TelemetryGatedError());
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    _resetOutboxForTest();
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  describe('outbox — recordEvent gated after the outbox already checked the switch', () => {
    it('keeps the entry queued instead of evicting it as sent', async () => {
      await enqueueCritical({ type: 'library_add', search_id: 'a' });

      expect(recordEventMock).toHaveBeenCalledTimes(1);
      expect(queuedIds()).toEqual(['a']);
    });

    it('does not log the gated entry as a send failure', async () => {
      await enqueueCritical({ type: 'library_add', search_id: 'a' });

      expect(console.warn).not.toHaveBeenCalled();
    });

    it('arms no backoff retry for a gated entry', async () => {
      await enqueueCritical({ type: 'library_add', search_id: 'a' });

      expect(jest.getTimerCount()).toBe(0);
    });

    it('sends the entry once recordEvent stops gating it', async () => {
      await enqueueCritical({ type: 'library_add', search_id: 'a' });
      recordEventMock.mockResolvedValue(undefined);

      await flushOutbox();

      expect(queuedIds()).toEqual([]);
    });
  });
});
