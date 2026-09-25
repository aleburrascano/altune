// Regression (#818): a malformed queue-state response is rejected at one named parse
// boundary and logged, instead of crashing mid-restore or silently restoring a queue
// with an unrecognized source reported as the library.

import { act, renderHook } from '@testing-library/react-native';

import { getQueueState } from '@shared/api-client/playback';
import { getAllTracks, getTracks } from '@shared/api-client/tracks';
import type { AcquisitionStatus, TrackResponse } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';

import { useQueueResume } from '../hooks/useQueueResume';

jest.mock('@shared/api-client/playback', () => ({
  getQueueState: jest.fn(),
  saveQueueState: jest.fn(async () => undefined),
}));
jest.mock('@shared/api-client/tracks', () => ({ getTracks: jest.fn(), getAllTracks: jest.fn() }));
jest.mock('@shared/api-client/audio', () => ({
  audioStreamUrl: (id: string) => `https://api.example/audio/${id}`,
  audioRequestHeaders: jest.fn(async () => ({})),
  fetchAudioUrls: jest.fn(async () => []),
}));

const mockedGetQueueState = getQueueState as jest.Mock;
const mockedGetTracks = getTracks as jest.Mock;
const mockedGetAllTracks = getAllTracks as jest.Mock;
const { __player } = jest.requireMock('react-native-track-player');

function trackResponse(id: string, acquisitionStatus: AcquisitionStatus = 'ready'): TrackResponse {
  return {
    id,
    title: `Title ${id}`,
    artist: 'Artist',
    album: null,
    duration_seconds: 200,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: acquisitionStatus,
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

async function settlePendingWork(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 40; i++) await Promise.resolve();
  });
}

async function settleRestore(): Promise<void> {
  renderHook(() => useQueueResume());
  await settlePendingWork();
}

async function restore(body: unknown): Promise<void> {
  mockedGetQueueState.mockResolvedValue(body);
  await settleRestore();
}

let warn: jest.SpyInstance;

beforeEach(() => {
  useQueueStore.getState().clearQueue();
  mockedGetQueueState.mockReset();
  mockedGetTracks.mockReset();
  mockedGetAllTracks.mockReset().mockResolvedValue(['x', 'y'].map((id) => trackResponse(id)));
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

  // Regression (#1736): a source kind a newer app version added must cost the restore only
  // the source, not the queue and position saved beside it.
  it('restores the saved queue with no source when source.kind is unrecognized', async () => {
    await restore({ ...validWire(), source: { kind: 'album' } });

    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(2);
    expect(s.currentIndex).toBe(1);
    expect(s.source).toBeNull();
    expect(warnedMalformed()).toBe(false);
  });

  it.each<[string, (w: Record<string, unknown>) => unknown]>([
    ['missing track_ids', ({ track_ids: _drop, ...rest }) => rest],
    ['non-array track_ids', (w) => ({ ...w, track_ids: 'x,y' })],
    ['non-string track id', (w) => ({ ...w, track_ids: ['x', 7] })],
    ['fractional current_index', (w) => ({ ...w, current_index: 0.5 })],
    ['negative current_index', (w) => ({ ...w, current_index: -1 })],
    ['string current_index', (w) => ({ ...w, current_index: '1' })],
    ['non-array natural_order', (w) => ({ ...w, natural_order: { 0: 'x' } })],
    ['non-object body', () => 'queue'],
  ])('rejects and logs a response with %s, restoring nothing', async (_label, mutate) => {
    await restore(mutate(validWire()));

    expect(warnedMalformed()).toBe(true);
    expect(mockedGetAllTracks).not.toHaveBeenCalled();
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

// Regression (#1743): the restore catch logged a bare string over a multi-stage chain, so
// "my queue never resumes" could not be told from the logs apart from a dead network.
describe('useQueueResume restore — a failure names its stage and carries the error', () => {
  function restoreFailureFields(): unknown {
    const call = warn.mock.calls.find(
      ([message]) => message === '[playback] failed to restore the saved queue',
    );
    return call?.[1];
  }

  it('blames the fetch stage when reading the saved queue state throws', async () => {
    const offline = new Error('network down');
    mockedGetQueueState.mockRejectedValue(offline);

    await settleRestore();

    expect(restoreFailureFields()).toEqual({
      stage: 'fetch',
      error: { kind: 'unknown', message: 'network down' },
    });
  });

  it('blames the tracks stage when the library read behind rehydration throws', async () => {
    const unavailable = new Error('tracks 503');
    mockedGetAllTracks.mockRejectedValue(unavailable);

    await restore(validWire());

    expect(restoreFailureFields()).toEqual({
      stage: 'tracks',
      error: { kind: 'unknown', message: 'tracks 503' },
    });
  });

  it('blames the native stage when handing the rebuilt queue to the player throws', async () => {
    const addRejected = new Error('native add rejected');
    __player.failNext('add', addRejected);

    await restore(validWire());

    expect(restoreFailureFields()).toEqual({
      stage: 'native',
      error: { kind: 'unknown', message: 'native add rejected' },
    });
  });
});

// Regression (#1726): the rehydration placeholder is a "now playing" card with nothing in the
// native player behind it, so a restore that stops before the native load left play, pause and
// seek as silent no-ops against a track the store still reported as current.
describe('useQueueResume restore — an unbacked placeholder is taken back down', () => {
  function savedWithCurrentTrack(): Record<string, unknown> {
    return {
      ...validWire(),
      current_track: {
        id: 'y',
        title: 'Title y',
        artist: 'Artist',
        artwork_url: null,
        duration_seconds: 200,
        acquisition_status: 'ready',
      },
    };
  }

  function clearedPlaceholderFields(): unknown {
    const call = warn.mock.calls.find(
      ([message]) => message === '[playback] cleared the unbacked resume placeholder',
    );
    return call?.[1];
  }

  function expectNothingPlaying(): void {
    const s = useQueueStore.getState();
    expect(s.currentTrack()).toBeNull();
    expect(s.tracks).toHaveLength(0);
    expect(__player.calls('add')).toHaveLength(0);
  }

  it('clears the placeholder it showed when the library fetch comes back empty', async () => {
    let resolveLibrary!: (tracks: TrackResponse[]) => void;
    mockedGetAllTracks.mockReturnValue(
      new Promise<TrackResponse[]>((resolve) => {
        resolveLibrary = resolve;
      }),
    );
    mockedGetQueueState.mockResolvedValue(savedWithCurrentTrack());

    await settleRestore();
    expect(useQueueStore.getState().currentTrack()?.title).toBe('Title y');

    resolveLibrary([]);
    await settlePendingWork();

    expectNothingPlaying();
    expect(clearedPlaceholderFields()).toEqual({ stage: 'tracks' });
  });

  it('clears the placeholder when no saved track is ready to rebuild from', async () => {
    mockedGetAllTracks.mockResolvedValue(['x', 'y'].map((id) => trackResponse(id, 'pending')));

    await restore(savedWithCurrentTrack());

    expectNothingPlaying();
    expect(clearedPlaceholderFields()).toEqual({ stage: 'rebuild' });
  });

  it('keeps the rebuilt queue when the restore runs through to the native load', async () => {
    await restore(savedWithCurrentTrack());

    expect(useQueueStore.getState().tracks).toHaveLength(2);
    expect(__player.calls('add')).not.toHaveLength(0);
    expect(clearedPlaceholderFields()).toBeUndefined();
  });
});

// Regression (#1740): the restore read one fixed 2000-track page of the library, so a saved
// queue referencing anything past that page came back short with nothing logged.
describe('useQueueResume restore — a saved queue larger than one library page', () => {
  const LIBRARY_PAGE = 2000;
  const LIBRARY_SIZE = 2500;

  function libraryTracks(size: number): TrackResponse[] {
    return Array.from({ length: size }, (_, i) => trackResponse(`t${i}`));
  }

  function savedWholeLibrary(
    tracks: TrackResponse[],
    currentIndex: number,
  ): Record<string, unknown> {
    const ids = tracks.map((t) => t.id);
    return { ...validWire(), track_ids: ids, natural_order: ids, current_index: currentIndex };
  }

  function missingFromLibraryFields(): unknown {
    const call = warn.mock.calls.find(
      ([message]) =>
        message ===
        '[playback] saved queue tracks missing from the library read; restoring without',
    );
    return call?.[1];
  }

  it('restores every saved track when the library spans more than one page', async () => {
    const tracks = libraryTracks(LIBRARY_SIZE);
    mockedGetAllTracks.mockResolvedValue(tracks);
    // What a single first page would answer — a restore reading only that page rebuilds
    // without the tail, and the store below says so.
    mockedGetTracks.mockResolvedValue({ items: tracks.slice(0, LIBRARY_PAGE), has_more: true });

    await restore(savedWholeLibrary(tracks, LIBRARY_SIZE - 1));

    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(LIBRARY_SIZE);
    expect(s.currentTrack()?.title).toBe(`Title t${LIBRARY_SIZE - 1}`);
    expect(warn).not.toHaveBeenCalled();
  });

  it('warns with the count of saved tracks the library read did not return', async () => {
    const tracks = libraryTracks(LIBRARY_SIZE);
    const firstPage = tracks.slice(0, LIBRARY_PAGE);
    mockedGetAllTracks.mockResolvedValue(firstPage);
    mockedGetTracks.mockResolvedValue({ items: firstPage, has_more: true });

    await restore(savedWholeLibrary(tracks, 0));

    expect(useQueueStore.getState().tracks).toHaveLength(LIBRARY_PAGE);
    expect(missingFromLibraryFields()).toEqual({
      missing: LIBRARY_SIZE - LIBRARY_PAGE,
      saved: LIBRARY_SIZE,
    });
  });
});
