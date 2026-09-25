import { _resetOutboxForTest, enqueueCritical, flushOutbox } from '../outbox';
import { loadPersistedOutbox, persistOutbox } from '../outboxStore';
import { recordEvent, TelemetryGatedError } from '../recordEvent';

jest.mock('../outboxStore', () => ({
  loadPersistedOutbox: jest.fn(),
  persistOutbox: jest.fn(),
}));

jest.mock('../recordEvent', () => ({
  ...jest.requireActual('../recordEvent'),
  recordEvent: jest.fn(),
}));

const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;
const persistOutboxMock = persistOutbox as jest.MockedFunction<typeof persistOutbox>;

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
