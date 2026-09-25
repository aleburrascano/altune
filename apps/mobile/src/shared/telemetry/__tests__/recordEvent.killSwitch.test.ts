import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';
import { supabase } from '@shared/auth/supabaseClient';

import { recordEvent, TelemetryGatedError } from '../recordEvent';
import { _resetOutboxForTest, enqueueCritical, flushOutbox } from '../outbox';
import { loadPersistedOutbox, persistOutbox } from '../outboxStore';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('../session', () => ({ getSessionId: jest.fn().mockReturnValue('session-1') }));

jest.mock('../outboxStore', () => ({
  loadPersistedOutbox: jest.fn(),
  persistOutbox: jest.fn(),
}));

const getSession = supabase.auth.getSession as jest.Mock;
const persistOutboxMock = persistOutbox as jest.MockedFunction<typeof persistOutbox>;

function queuedIds(): (string | undefined)[] {
  return (persistOutboxMock.mock.calls.at(-1)?.[0] ?? []).map((e) => e.search_id);
}

async function settle(): Promise<void> {
  for (let i = 0; i < 20; i += 1) await Promise.resolve();
}

beforeEach(() => {
  getSession.mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null });
  setKillSwitchFileStore(createMemoryFileStore());
  _resetOutboxForTest();
  (loadPersistedOutbox as jest.Mock).mockReset().mockReturnValue([]);
  persistOutboxMock.mockReset();
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  _resetOutboxForTest();
  setKillSwitchFileStore();
  jest.restoreAllMocks();
});

describe('recordEvent() — remote telemetry kill switch', () => {
  it('sends nothing while the switch is off', async () => {
    applyKillSwitches({ telemetry_enabled: false });

    await expect(recordEvent({ type: 'results_shown' })).rejects.toThrow(TelemetryGatedError);

    expect(__http.requests.length).toBe(0);
  });

  it('rejects with the gated error while the switch is off, without sending', async () => {
    applyKillSwitches({ telemetry_enabled: false });

    await expect(recordEvent({ type: 'result_clicked' })).rejects.toThrow(TelemetryGatedError);
  });

  it('sends again once the switch is turned back on', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    applyKillSwitches({ telemetry_enabled: false });

    await expect(recordEvent({ type: 'play' })).rejects.toThrow(TelemetryGatedError);
    expect(__http.requests.length).toBe(0);

    applyKillSwitches({ telemetry_enabled: true });
    await recordEvent({ type: 'play' });

    expect(__http.requests.length).toBe(1);
  });

  it('leaves an outbox entry queued while the switch is off and flushes it once re-enabled', async () => {
    applyKillSwitches({ telemetry_enabled: false });

    await enqueueCritical({ type: 'library_add', search_id: 'a' });
    await flushOutbox();

    expect(__http.requests.length).toBe(0);
    expect(queuedIds()).toEqual(['a']);

    __http.reply('POST /v1/discovery/events', { status: 202 });
    applyKillSwitches({ telemetry_enabled: true });
    await settle();

    expect(__http.requests.length).toBe(1);
    expect(queuedIds()).toEqual([]);
  });
});
