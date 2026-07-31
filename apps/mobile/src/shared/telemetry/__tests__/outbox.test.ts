import { ApiError, NetworkError } from '@shared/api-client';

import type { OutboxEntry } from '../outbox';
import { enqueueCritical, flushOutbox, _resetOutboxForTest } from '../outbox';
import { loadPersistedOutbox, persistOutbox } from '../outboxStore';
import { recordEvent, type DiscoveryEvent } from '../recordEvent';

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
  _resetOutboxForTest();
  loadPersistedOutboxMock.mockReset().mockReturnValue([]);
  persistOutboxMock.mockReset();
  recordEventMock.mockReset().mockResolvedValue(undefined);
});

describe('Reducer: enqueueCritical', () => {
  it('enqueuing onto an empty queue commits the new entry to disk before its send has resolved', () => {
    recordEventMock.mockReturnValue(new Promise(() => {}));

    enqueueCritical(event({ query_norm: 'first' })).catch(() => undefined);

    expect(lastPersisted()?.map((e) => e.query_norm)).toEqual(['first']);
  });

  it('enqueuing past MAX_ENTRIES caps the queue at MAX_ENTRIES, dropping the oldest entry', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

    for (let i = 0; i <= MAX_ENTRIES; i += 1) {
      await enqueueCritical(event({ query_norm: `q${i}` }));
    }

    const persisted = lastPersisted();
    expect(persisted).toHaveLength(MAX_ENTRIES);
    expect(persisted?.some((e) => e.query_norm === 'q0')).toBe(false);
    expect(persisted?.some((e) => e.query_norm === `q${MAX_ENTRIES}`)).toBe(true);
  });

  it('enqueuing an entry whose minted event_id collides with one already queued overwrites it rather than duplicating it', async () => {
    const randomSpy = jest.spyOn(Math, 'random').mockReturnValue(0);
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

    await enqueueCritical(event({ query_norm: 'first' }));
    await enqueueCritical(event({ query_norm: 'second' }));
    randomSpy.mockRestore();

    const persisted = lastPersisted();
    expect(persisted).toHaveLength(1);
    expect(persisted?.[0]?.query_norm).toBe('second');
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
    await enqueueCritical(event({ query_norm: 'a' }));
    await enqueueCritical(event({ query_norm: 'b' }));
    recordEventMock.mockReset().mockResolvedValue(undefined);

    await flushOutbox();

    expect(sentEntries().map((e) => e.query_norm)).toEqual(['a', 'b']);
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
    await enqueueCritical(event({ query_norm: 'only' }));

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
    await enqueueCritical(event({ query_norm: 'only' }));
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
    const randomSpy = jest.spyOn(Math, 'random').mockReturnValue(0.5);
    recordEventMock.mockRejectedValue(new Error('send unavailable'));

    await enqueueCritical(event({ type: 'wrong_album', query_norm: 'replay' }));
    const afterOnce = lastPersisted();

    await enqueueCritical(event({ type: 'wrong_album', query_norm: 'replay' }));
    const afterTwice = lastPersisted();
    randomSpy.mockRestore();

    expect(afterOnce).toHaveLength(1);
    expect(afterTwice).toHaveLength(1);
    expect(afterTwice?.[0]?.event_id).toBe(afterOnce?.[0]?.event_id);
  });
});

describe('Idempotence: flushing an already-drained queue', () => {
  it('is a no-op, identical to the drain that already happened', async () => {
    await enqueueCritical(event({ query_norm: 'drains' }));
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

    await freshEnqueue(event({ query_norm: 'q' }));
    expect(freshRecordEvent).toHaveBeenCalledTimes(1);

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

    await freshEnqueue(event({ query_norm: 'q' }));
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

    await freshEnqueue(event({ query_norm: 'first' }));
    await freshEnqueue(event({ query_norm: 'second' }));
    await freshEnqueue(event({ query_norm: 'third' }));

    expect(listeners.length - listenerCountBefore).toBe(1);
  });
});

describe('Failure injection: recordEvent is the one I/O call site', () => {
  it('rejecting on the first entry attempts only that entry and leaves the whole queue, in order, on disk', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ query_norm: 'a' }));
    await enqueueCritical(event({ query_norm: 'b' }));
    recordEventMock.mockClear();

    await flushOutbox();

    expect(recordEventMock).toHaveBeenCalledTimes(1);
    expect(sentEntries()[0]?.query_norm).toBe('a');
    expect(lastPersisted()?.map((e) => e.query_norm)).toEqual(['a', 'b']);
  });

  it('rejecting on a middle entry commits everything sent before it and leaves the failure and everything after it queued', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ query_norm: 'a' }));
    await enqueueCritical(event({ query_norm: 'b' }));
    await enqueueCritical(event({ query_norm: 'c' }));
    recordEventMock.mockReset();
    recordEventMock.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('still down'));

    await flushOutbox();

    expect(lastPersisted()?.map((e) => e.query_norm)).toEqual(['b', 'c']);
  });

  it('rejecting on every entry across repeated flush calls never drains the queue and always retries from the front', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ query_norm: 'a' }));
    await enqueueCritical(event({ query_norm: 'b' }));
    recordEventMock.mockClear();

    await flushOutbox();
    await flushOutbox();
    await flushOutbox();

    expect(recordEventMock).toHaveBeenCalledTimes(3);
    expect(sentEntries().every((e) => e.query_norm === 'a')).toBe(true);
    expect(lastPersisted()?.map((e) => e.query_norm)).toEqual(['a', 'b']);
  });
});

describe('Regression: a permanently rejected entry must not block the queue behind it', () => {
  it('drops an entry the server rejects with 400 and keeps draining the rest', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ query_norm: 'poisoned' }));
    await enqueueCritical(event({ query_norm: 'b' }));
    await enqueueCritical(event({ query_norm: 'c' }));

    recordEventMock.mockReset();
    recordEventMock.mockImplementation(async (entry: DiscoveryEvent) => {
      if (entry.query_norm === 'poisoned') throw new ApiError(400, 'payload.result_signature must be a string');
      return undefined;
    });

    await flushOutbox();

    expect(sentEntries().map((e) => e.query_norm)).toEqual(['poisoned', 'b', 'c']);
    expect(lastPersisted()).toEqual([]);
  });

  it('still stops at the first retryable failure, so a transient outage never discards an entry', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ query_norm: 'a' }));
    await enqueueCritical(event({ query_norm: 'b' }));

    recordEventMock.mockReset();
    recordEventMock.mockRejectedValue(new NetworkError('transport', 'unreachable'));

    await flushOutbox();

    expect(recordEventMock).toHaveBeenCalledTimes(1);
    expect(lastPersisted()?.map((e) => e.query_norm)).toEqual(['a', 'b']);
  });

  it('retries a 401 rather than dropping it, so a briefly absent session never loses a label', async () => {
    recordEventMock.mockRejectedValue(new Error('send unavailable'));
    await enqueueCritical(event({ query_norm: 'a' }));

    recordEventMock.mockReset();
    recordEventMock.mockRejectedValue(new ApiError(401, 'no active session'));

    await flushOutbox();

    expect(lastPersisted()?.map((e) => e.query_norm)).toEqual(['a']);
  });
});
