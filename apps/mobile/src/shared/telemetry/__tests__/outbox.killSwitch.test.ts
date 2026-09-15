// Regression for issue #955: the telemetry outbox must not send while its remote kill switch is
// off, so repeated failing POSTs can be stopped without an app release.

import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

import { _resetOutboxForTest, enqueueCritical, flushOutbox } from '../outbox';
import { loadPersistedOutbox, persistOutbox } from '../outboxStore';
import { recordEvent } from '../recordEvent';

jest.mock('../outboxStore', () => ({
  loadPersistedOutbox: jest.fn(),
  persistOutbox: jest.fn(),
}));

jest.mock('../recordEvent', () => ({ recordEvent: jest.fn() }));

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

const { __listeners: appStateListeners } = jest.requireMock(
  'react-native/Libraries/AppState/AppState',
) as { __listeners: AppStateChangeHandler[] };

const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;
const persistOutboxMock = persistOutbox as jest.MockedFunction<typeof persistOutbox>;

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
