import { State } from 'react-native-track-player';

import { applyKillSwitches } from '@shared/killSwitch/killSwitch';
import { signInTestUser } from '@testing/signInTestUser';

import {
  markPlayRequested,
  notePlaybackState,
  resetPlayStartTiming,
} from '@features/playback/native/playStartTiming';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const SILENT_MS = 1200;
const GIVE_UP_MS = 60_000;

interface SentRequest {
  method: string;
  path: string;
  body?: string;
}

function sentPlayStarts(): Record<string, unknown>[] {
  return __http.requests
    .filter((r: SentRequest) => r.method === 'POST' && r.path === '/v1/discovery/events')
    .map((r: SentRequest) => JSON.parse(r.body ?? '{}'))
    .filter((body: { type?: string }) => body.type === 'play_start')
    .map((body: { payload: Record<string, unknown> }) => body.payload);
}

async function waitMs(ms: number): Promise<void> {
  await jest.advanceTimersByTimeAsync(ms);
}

beforeEach(async () => {
  await signInTestUser();
  applyKillSwitches({ telemetry_enabled: true });
  __http.reply(EVENTS, { status: 202 });
  resetPlayStartTiming();
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
  resetPlayStartTiming();
});

describe('play_start: time from a play request until the player reports playing', () => {
  it('sends one event with the silent wait, the trigger, and cold for the first start', async () => {
    markPlayRequested('remote');
    await waitMs(SILENT_MS);
    notePlaybackState(State.Playing);
    await waitMs(0);

    expect(sentPlayStarts()).toEqual([
      expect.objectContaining({
        outcome: 'playing',
        trigger: 'remote',
        cold: true,
        start_ms: SILENT_MS,
      }),
    ]);
  });

  it('marks later starts as warm', async () => {
    markPlayRequested('play');
    notePlaybackState(State.Playing);
    notePlaybackState(State.Paused);
    markPlayRequested('resume');
    await waitMs(SILENT_MS);
    notePlaybackState(State.Playing);
    await waitMs(0);

    expect(sentPlayStarts().map((p) => p.cold)).toEqual([true, false]);
  });

  it('counts a retry while still silent from the first request', async () => {
    markPlayRequested('play');
    await waitMs(SILENT_MS);
    markPlayRequested('play');
    await waitMs(SILENT_MS);
    notePlaybackState(State.Playing);
    await waitMs(0);

    expect(sentPlayStarts()).toEqual([expect.objectContaining({ start_ms: 2 * SILENT_MS })]);
  });

  it('ignores a remote play or resume while audio is already playing', async () => {
    markPlayRequested('play');
    notePlaybackState(State.Playing);
    markPlayRequested('remote');
    markPlayRequested('resume');
    await waitMs(GIVE_UP_MS);

    expect(sentPlayStarts()).toHaveLength(1);
  });

  it('measures a switch to a new track while another one plays', async () => {
    markPlayRequested('play');
    notePlaybackState(State.Playing);
    markPlayRequested('queue');
    notePlaybackState(State.Loading);
    await waitMs(SILENT_MS);
    notePlaybackState(State.Playing);
    await waitMs(0);

    expect(sentPlayStarts()).toEqual([
      expect.objectContaining({ trigger: 'play' }),
      expect.objectContaining({ trigger: 'queue', cold: false, start_ms: SILENT_MS }),
    ]);
  });

  it('reports never_started when buffering never turns into playing', async () => {
    markPlayRequested('remote');
    notePlaybackState(State.Buffering);
    await waitMs(GIVE_UP_MS);

    expect(sentPlayStarts()).toEqual([
      expect.objectContaining({ outcome: 'never_started', start_ms: GIVE_UP_MS }),
    ]);
  });

  it('drops a pending start on sign-out, so no event is sent for the next account', async () => {
    markPlayRequested('play');
    resetPlayStartTiming();
    await waitMs(GIVE_UP_MS);

    expect(sentPlayStarts()).toEqual([]);
  });

  it('sends nothing while the telemetry kill switch is off', async () => {
    applyKillSwitches({ telemetry_enabled: false });
    markPlayRequested('play');
    notePlaybackState(State.Playing);
    await waitMs(0);

    expect(sentPlayStarts()).toEqual([]);
  });
});
