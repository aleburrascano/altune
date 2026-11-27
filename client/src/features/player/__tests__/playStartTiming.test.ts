import { State } from 'react-native-track-player';

import { signInTestUser } from '@testing/signInTestUser';

import {
  _resetPlaybackHealthForTest,
  flushPlaybackHealth,
  markPlayRequested,
  notePlaybackState,
  resetPlayStartTiming,
} from '@features/player';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const SILENT_MS = 1200;
const GIVE_UP_MS = 60_000;

interface SentRequest {
  method: string;
  path: string;
  body?: string;
}

interface SentEvent {
  type?: string;
  payload: Record<string, unknown>;
}

function sentPlayStarts(): Record<string, unknown>[] {
  return __http.requests
    .filter((r: SentRequest) => r.method === 'POST' && r.path === '/v1/discovery/events')
    .map((r: SentRequest) => JSON.parse(r.body ?? '{}'))
    .filter((body: SentEvent) => body.type === 'play_start')
    .map((body: SentEvent) => body.payload);
}

function sentHealth(): Record<string, unknown>[] {
  return __http.requests
    .filter((r: SentRequest) => r.method === 'POST' && r.path === '/v1/discovery/events')
    .map((r: SentRequest) => JSON.parse(r.body ?? '{}'))
    .filter((body: SentEvent) => body.type === 'playback_health')
    .map((body: SentEvent) => body.payload);
}

async function waitMs(ms: number): Promise<void> {
  await jest.advanceTimersByTimeAsync(ms);
}

beforeEach(async () => {
  await signInTestUser();
  __http.reply(EVENTS, { status: 202 });
  resetPlayStartTiming();
  _resetPlaybackHealthForTest();
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

    expect(sentPlayStarts()).toEqual([
      expect.objectContaining({ start_ms: SILENT_MS + SILENT_MS }),
    ]);
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
});

describe('play_attempt, play_ok and the fast or slow start: the play-a-song SLI in the health tally', () => {
  const FAST_MS = 500;
  const SLOW_MS = 3000;

  async function healthAfter(run: () => Promise<void>): Promise<Record<string, unknown>[]> {
    await run();
    flushPlaybackHealth();
    await waitMs(0);
    return sentHealth();
  }

  it('counts an attempt that reaches audio quickly as ok and fast', async () => {
    const health = await healthAfter(async () => {
      markPlayRequested('play');
      await waitMs(FAST_MS);
      notePlaybackState(State.Playing);
    });

    expect(health).toEqual([
      expect.objectContaining({
        play_attempt: 1,
        play_ok: 1,
        play_start_fast: 1,
        play_start_slow: 0,
      }),
    ]);
  });

  it('counts an attempt that takes long as ok and slow', async () => {
    const health = await healthAfter(async () => {
      markPlayRequested('play');
      await waitMs(SLOW_MS);
      notePlaybackState(State.Playing);
    });

    expect(health).toEqual([
      expect.objectContaining({
        play_attempt: 1,
        play_ok: 1,
        play_start_fast: 0,
        play_start_slow: 1,
      }),
    ]);
  });

  it('counts an attempt that never reaches audio as an attempt only', async () => {
    const health = await healthAfter(async () => {
      markPlayRequested('play');
      await waitMs(GIVE_UP_MS);
    });

    expect(health).toEqual([
      expect.objectContaining({
        play_attempt: 1,
        play_ok: 0,
        play_start_fast: 0,
        play_start_slow: 0,
      }),
    ]);
  });

  it('counts a retry while still silent once, like the play_start event', async () => {
    const health = await healthAfter(async () => {
      markPlayRequested('play');
      markPlayRequested('play');
      notePlaybackState(State.Playing);
    });

    expect(health).toEqual([expect.objectContaining({ play_attempt: 1, play_ok: 1 })]);
  });
});
