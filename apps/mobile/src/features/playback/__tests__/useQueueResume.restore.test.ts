// Regression (#818): a malformed queue-state response is rejected at one named parse
// boundary and logged, instead of crashing mid-restore or silently restoring a queue
// with an unrecognized source reported as the library.

import { act, renderHook } from '@testing-library/react-native';

import { getQueueState } from '@shared/api-client/playback';
import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';

import { useQueueResume } from '../hooks/useQueueResume';

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: jest.fn(),
  saveQueueState: jest.fn(async () => undefined),
}));
jest.mock('@shared/api-client/tracks', () => ({ getTracks: jest.fn() }));
jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

const mockedGetQueueState = getQueueState as jest.Mock;
const mockedGetTracks = getTracks as jest.Mock;

function trackResponse(id: string): TrackResponse {
  return {
    id,
    title: `Title ${id}`,
    artist: 'Artist',
    album: null,
    duration_seconds: 200,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  } as TrackResponse;
}

function validWire(): Record<string, unknown> {
  return {
    track_ids: ['x', 'y'],
    current_index: 1,
    position_ms: 5000,
    shuffled: false,
    repeat_mode: 'off',
    source: { kind: 'playlist', playlist_id: 'p1', name: 'Chill' },
    natural_order: ['x', 'y'],
  };
}

async function restore(body: unknown): Promise<void> {
  mockedGetQueueState.mockResolvedValue(body);
  renderHook(() => useQueueResume());
  await act(async () => {
    for (let i = 0; i < 40; i++) await Promise.resolve();
  });
}

let warn: jest.SpyInstance;

beforeEach(() => {
  useQueueStore.getState().clearQueue();
  mockedGetTracks.mockReset().mockResolvedValue({
    items: ['x', 'y'].map(trackResponse),
    has_more: false,
  });
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.restoreAllMocks();
});

function warnedMalformed(): boolean {
  return warn.mock.calls.some(([message]) =>
    String(message).includes('rejected a malformed saved queue state'),
  );
}

describe('useQueueResume restore — queue-state parse boundary', () => {
  it('restores a well-formed saved queue without warning', async () => {
    await restore(validWire());

    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(2);
    expect(s.currentIndex).toBe(1);
    expect(s.source).toEqual({ kind: 'playlist', playlistId: 'p1', name: 'Chill' });
    expect(warn).not.toHaveBeenCalled();
  });

  it.each<[string, (w: Record<string, unknown>) => unknown]>([
    ['missing track_ids', ({ track_ids: _drop, ...rest }) => rest],
    ['non-array track_ids', (w) => ({ ...w, track_ids: 'x,y' })],
    ['non-string track id', (w) => ({ ...w, track_ids: ['x', 7] })],
    ['fractional current_index', (w) => ({ ...w, current_index: 0.5 })],
    ['negative current_index', (w) => ({ ...w, current_index: -1 })],
    ['string current_index', (w) => ({ ...w, current_index: '1' })],
    ['unrecognized source.kind', (w) => ({ ...w, source: { kind: 'album' } })],
    ['non-array natural_order', (w) => ({ ...w, natural_order: { 0: 'x' } })],
    ['non-object body', () => 'queue'],
  ])('rejects and logs a response with %s, restoring nothing', async (_label, mutate) => {
    await restore(mutate(validWire()));

    expect(warnedMalformed()).toBe(true);
    expect(mockedGetTracks).not.toHaveBeenCalled();
    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(0);
    expect(s.source).toBeNull();
  });

  it('accepts a legacy row with no natural_order by rebuilding from the play order', async () => {
    const legacy = validWire();
    delete legacy.natural_order;
    await restore({ ...legacy, source: null });

    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(2);
    expect(s.currentIndex).toBe(1);
    expect(warn).not.toHaveBeenCalled();
  });
});
