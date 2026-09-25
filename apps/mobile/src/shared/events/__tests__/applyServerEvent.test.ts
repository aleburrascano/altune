import { QueryClient, type InfiniteData } from '@tanstack/react-query';

import {
  registerAudioCacheInvalidator,
  _resetAudioCacheInvalidatorsForTest,
} from '@shared/acquisition/audioCacheInvalidation';
import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { trackIdentityKey, useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type {
  ListPlaylistsResponse,
  ListTracksResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { applyServerEvent } from '../applyServerEvent';
import type { ServerEvent } from '../sse-client';
import { settleTrackPatches } from './settleTrackPatches';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn().mockResolvedValue([]) }));

type TrackPages = InfiniteData<ListTracksResponse>;

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function serverEvent(type: string, data: Record<string, unknown> = {}): ServerEvent {
  return { id: '1', type, data };
}

function invalidatedKeys(spy: jest.SpyInstance): unknown[] {
  return spy.mock.calls.map(([filters]) => (filters as { queryKey: unknown }).queryKey);
}

function trackFixture(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId('t1'),
    title: 'Original Title',
    artist: 'Original Artist',
    album: null,
    duration_seconds: null,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    ...overrides,
  } as TrackResponse;
}

function seedTrackPages(
  queryClient: QueryClient,
  items: TrackResponse[],
  total = items.length,
): readonly unknown[] {
  const key = libraryKeys.tracks('', 'recent');
  const data: TrackPages = {
    pageParams: [0],
    pages: [{ items, total, limit: 20, offset: 0, has_more: false }],
  };
  queryClient.setQueryData(key, data);
  return key;
}

function readTrackPages(queryClient: QueryClient, key: readonly unknown[]): ListTracksResponse {
  return (queryClient.getQueryData(key) as TrackPages).pages[0]!;
}

function playlistDetailFixture(
  overrides: Partial<PlaylistDetailResponse> = {},
): PlaylistDetailResponse {
  return {
    id: asPlaylistId('p1'),
    name: 'Old Name',
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks: [],
    ...overrides,
  };
}

function playlistSummaryFixture(overrides: Partial<PlaylistResponse> = {}): PlaylistResponse {
  return {
    id: asPlaylistId('p1'),
    name: 'Old Name',
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

beforeEach(() => {
  useTrackStatusStore.getState().reset();
  useDownloadStore.getState().reset();
  _resetAudioCacheInvalidatorsForTest();
});

afterEach(() => {
  useDownloadStore.getState().reset();
});

describe('unrecognized event type', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('warns instead of throwing or invalidating anything', () => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    expect(() =>
      applyServerEvent(queryClient, serverEvent('track_favourited', { track_id: 't1' })),
    ).not.toThrow();

    expect(warn).toHaveBeenCalledWith(expect.any(String), { type: 'track_favourited' });
    expect(spy).not.toHaveBeenCalled();
  });
});

describe('resync', () => {
  it('invalidates exactly the resync key set, in the declared order, once each', () => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    applyServerEvent(queryClient, serverEvent('resync'));

    expect(invalidatedKeys(spy)).toEqual([
      libraryKeys.tracksPrefix,
      libraryKeys.lookupPrefix,
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.featuringPrefix,
      playlistKeys.list,
      playlistKeys.details,
    ]);
  });
});

describe('track_added_to_library', () => {
  it('upserts a fully-populated track into a seeded page and links its identity', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);

    applyServerEvent(
      queryClient,
      serverEvent('track_added_to_library', {
        id: 't1',
        title: 'Song Title',
        artist: 'The Artist',
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'pending',
        album: 'The Album',
        duration_seconds: 210,
        artwork_url: 'https://cdn/art.png',
        year: 2020,
        genre: 'Rock',
        track_number: 3,
        album_artist: 'Album Artist',
        isrc: 'ISRC1',
      }),
    );

    const page = readTrackPages(queryClient, key);
    expect(page.items[0]).toEqual(
      trackFixture({
        id: asTrackId('t1'),
        title: 'Song Title',
        artist: 'The Artist',
        album: 'The Album',
        duration_seconds: 210,
        artwork_url: 'https://cdn/art.png',
        year: 2020,
        genre: 'Rock',
        track_number: 3,
        album_artist: 'Album Artist',
        isrc: 'ISRC1',
      }),
    );
    expect(page.total).toBe(1);

    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'pending',
      failureMessage: null,
    });
    const identity = trackIdentityKey('Song Title', 'The Artist')!;
    expect(useTrackStatusStore.getState().identities[identity]).toBe('t1');
  });

  it('defaults every optional field to null when the thin payload omits it', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);

    applyServerEvent(
      queryClient,
      serverEvent('track_added_to_library', {
        id: 't1',
        title: 'Song Title',
        artist: 'The Artist',
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'pending',
      }),
    );

    const page = readTrackPages(queryClient, key);
    expect(page.items[0]).toEqual(
      trackFixture({ id: asTrackId('t1'), title: 'Song Title', artist: 'The Artist' }),
    );
  });

  it('falls back to the legacy track_id field when id is absent', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);

    applyServerEvent(
      queryClient,
      serverEvent('track_added_to_library', {
        track_id: 't2',
        title: 'Song Title',
        artist: 'The Artist',
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'pending',
      }),
    );

    expect(readTrackPages(queryClient, key).items[0]!.id).toBe('t2');
  });

  it('coerces a numeric field carrying the wrong JSON type to null instead of stringifying it', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);

    applyServerEvent(
      queryClient,
      serverEvent('track_added_to_library', {
        id: 't1',
        title: 'Song Title',
        artist: 'The Artist',
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'pending',
        duration_seconds: '210',
      }),
    );

    expect(readTrackPages(queryClient, key).items[0]!.duration_seconds).toBeNull();
  });

  it.each<[string, Record<string, unknown>]>([
    [
      'both id and track_id',
      { title: 't', artist: 'a', added_at: 'd', acquisition_status: 'pending' },
    ],
    ['title', { id: '1', artist: 'a', added_at: 'd', acquisition_status: 'pending' }],
    ['artist', { id: '1', title: 't', added_at: 'd', acquisition_status: 'pending' }],
    ['added_at', { id: '1', title: 't', artist: 'a', acquisition_status: 'pending' }],
    ['acquisition_status', { id: '1', title: 't', artist: 'a', added_at: 'd' }],
  ])('falls back to refetch-invalidation when the payload is missing %s', (_label, payload) => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    applyServerEvent(queryClient, serverEvent('track_added_to_library', payload));

    expect(invalidatedKeys(spy)).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.lookupPrefix,
      libraryKeys.tracksPrefix,
      libraryKeys.featuringPrefix,
    ]);
  });

  it('rejects an off-contract acquisition_status instead of seeding the cache with it', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    applyServerEvent(
      queryClient,
      serverEvent('track_added_to_library', {
        id: 't1',
        title: 'Song Title',
        artist: 'The Artist',
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'queued',
      }),
    );

    expect(readTrackPages(queryClient, key).items).toHaveLength(0);
    expect(useTrackStatusStore.getState().statuses.t1).toBeUndefined();
    expect(invalidatedKeys(spy)).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.lookupPrefix,
      libraryKeys.tracksPrefix,
      libraryKeys.featuringPrefix,
    ]);
  });

  it('never upserts a blank-id, blank-artist track when a required field is the wrong JSON type', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);

    applyServerEvent(
      queryClient,
      serverEvent('track_added_to_library', {
        id: 't1',
        title: 'Song Title',
        artist: 42,
        added_at: '2026-01-01T00:00:00Z',
        acquisition_status: 'pending',
      }),
    );

    expect(readTrackPages(queryClient, key).items).toHaveLength(0);
  });

  it('replaying the same event twice does not duplicate the track', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, []);
    const payload = {
      id: 't1',
      title: 'Song Title',
      artist: 'The Artist',
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'pending',
    };

    applyServerEvent(queryClient, serverEvent('track_added_to_library', payload));
    applyServerEvent(queryClient, serverEvent('track_added_to_library', payload));

    const page = readTrackPages(queryClient, key);
    expect(page.items).toHaveLength(1);
    expect(page.total).toBe(1);
  });
});

describe('track_deleted', () => {
  it('removes the track from library and playlist caches and clears its status', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [trackFixture({ id: asTrackId('t1') })], 1);
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({ tracks: [trackFixture({ id: asTrackId('t1') })], track_count: 1 }),
    );
    useTrackStatusStore
      .getState()
      .patch(asTrackId('t1'), { acquisitionStatus: 'ready', failureMessage: null });

    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    applyServerEvent(queryClient, serverEvent('track_deleted', { track_id: 't1' }));

    const page = readTrackPages(queryClient, key);
    expect(page.items).toHaveLength(0);
    expect(page.total).toBe(0);
    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!
        .tracks,
    ).toHaveLength(0);
    expect(useTrackStatusStore.getState().statuses.t1).toBeUndefined();
    expect(invalidatedKeys(spy)).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.lookupPrefix,
      playlistKeys.list,
    ]);
  });

  it('still invalidates library and playlist summaries when track_id is missing', () => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    applyServerEvent(queryClient, serverEvent('track_deleted', {}));

    expect(invalidatedKeys(spy)).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.lookupPrefix,
      playlistKeys.list,
    ]);
  });

  it('replaying the same deletion twice leaves the cache empty rather than erroring', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [trackFixture({ id: asTrackId('t1') })], 1);

    applyServerEvent(queryClient, serverEvent('track_deleted', { track_id: 't1' }));
    applyServerEvent(queryClient, serverEvent('track_deleted', { track_id: 't1' }));

    const page = readTrackPages(queryClient, key);
    expect(page.items).toHaveLength(0);
    expect(page.total).toBe(0);
  });
});

describe('track_acquisition_started', () => {
  it('ignores a track_id outside the TrackId shape instead of seeding the stores with it', () => {
    const queryClient = makeClient();

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_started', { track_id: '../escape' }),
    );

    expect(useDownloadStore.getState().entries).toEqual({});
    expect(useTrackStatusStore.getState().statuses).toEqual({});
  });

  it('starts the download entry blank and marks the track pending when nothing is cached', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

    expect(useDownloadStore.getState().entries.t1).toEqual({
      trackId: 't1',
      phase: 'finding',
      title: null,
      artist: null,
      artworkUrl: null,
    });
    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'pending',
      failureMessage: null,
    });
  });

  it('carries the cached title, artist and artwork into the new download entry', () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [
      trackFixture({
        id: asTrackId('t1'),
        title: 'Known Title',
        artist: 'Known Artist',
        artwork_url: 'art.png',
        acquisition_status: 'failed',
        failure_reason: 'no_source',
      }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

    expect(useDownloadStore.getState().entries.t1).toEqual({
      trackId: 't1',
      phase: 'finding',
      title: 'Known Title',
      artist: 'Known Artist',
      artworkUrl: 'art.png',
    });
  });

  it('clears a prior failure and reverts the cached track to pending', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({
        id: asTrackId('t1'),
        acquisition_status: 'failed',
        failure_reason: 'no_source',
      }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));
    await settleTrackPatches();

    const patched = readTrackPages(queryClient, key).items[0]!;
    expect(patched.acquisition_status).toBe('pending');
    expect(patched.failure_reason).toBeNull();
  });

  it('is a no-op when track_id is missing from the payload', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', {}));

    expect(useDownloadStore.getState().entries).toEqual({});
    expect(useTrackStatusStore.getState().statuses).toEqual({});
  });

  it('leaves the track ready when a started event is replayed after its completion (#1784)', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'pending' }),
    ]);
    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-1' }),
    );
    await settleTrackPatches();

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));
    await settleTrackPatches();

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('ready');
    expect(useTrackStatusStore.getState().statuses.t1?.acquisitionStatus).toBe('ready');
  });

  it('ignores a started event once the status store holds the track at ready, long after the download entry is gone', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'ready' }),
    ]);
    useTrackStatusStore
      .getState()
      .patch(asTrackId('t1'), { acquisitionStatus: 'ready', failureMessage: null });

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));
    await settleTrackPatches();

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('ready');
    expect(useDownloadStore.getState().entries.t1).toBeUndefined();
  });

  it('keeps the download at the phase it reached when a started event arrives out of order', () => {
    const queryClient = makeClient();
    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_progress', { track_id: 't1', stage: 'download' }),
    );

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

    expect(useDownloadStore.getState().entries.t1?.phase).toBe('downloading');
  });
});

describe('track_acquisition_progress', () => {
  it.each<[string | undefined, string | undefined]>([
    ['search', 'finding'],
    ['select', 'finding'],
    ['download', 'downloading'],
    ['tag', 'finishing'],
    ['store', 'finishing'],
    ['update_track', 'finishing'],
    ['some_unrecognized_stage', undefined],
    [undefined, undefined],
  ])('routes stage %s to download phase %s', (stage, expectedPhase) => {
    const queryClient = makeClient();
    const data: Record<string, unknown> = { track_id: 't1' };
    if (stage !== undefined) data.stage = stage;

    applyServerEvent(queryClient, serverEvent('track_acquisition_progress', data));

    if (expectedPhase === undefined) {
      expect(useDownloadStore.getState().entries.t1).toBeUndefined();
    } else {
      expect(useDownloadStore.getState().entries.t1?.phase).toBe(expectedPhase);
    }
  });

  it('is a no-op when track_id is missing from the payload', () => {
    const queryClient = makeClient();

    applyServerEvent(queryClient, serverEvent('track_acquisition_progress', { stage: 'download' }));

    expect(useDownloadStore.getState().entries).toEqual({});
  });

  it('does not regress the phase when a stale progress event arrives after a later one', () => {
    const queryClient = makeClient();

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_progress', { track_id: 't1', stage: 'tag' }),
    );
    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_progress', { track_id: 't1', stage: 'download' }),
    );

    expect(useDownloadStore.getState().entries.t1?.phase).toBe('finishing');
  });
});

describe('track_acquisition_completed', () => {
  it('marks the track ready with the new audio_ref and finishes the download', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'pending', audio_ref: null }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );
    await settleTrackPatches();

    const patched = readTrackPages(queryClient, key).items[0]!;
    expect(patched.acquisition_status).toBe('ready');
    expect(patched.audio_ref).toBe('ref-123');
    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'ready',
      failureMessage: null,
    });
    expect(useDownloadStore.getState().entries.t1?.phase).toBe('finishing');
  });

  it('clears the failure text of a track that completes without a started event first (#933)', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({
        id: asTrackId('t1'),
        acquisition_status: 'failed',
        failure_reason: 'no_source',
        failure_message: 'No source found',
      }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );
    await settleTrackPatches();

    const track = readTrackPages(queryClient, key).items[0]!;
    expect(track.acquisition_status).toBe('ready');
    expect(track.failure_reason).toBeNull();
    expect(track.failure_message).toBeNull();
  });

  it('keeps a previously-set audio_ref when a thin completion event omits it', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'pending', audio_ref: 'old-ref' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_completed', { track_id: 't1' }));
    await settleTrackPatches();

    const track = readTrackPages(queryClient, key).items[0]!;
    expect(track.acquisition_status).toBe('ready');
    expect(track.audio_ref).toBe('old-ref');
  });

  it('notifies registered audio cache invalidators with the completed trackId', () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [trackFixture({ id: asTrackId('t1') })]);
    const notified: string[] = [];
    const unregister = registerAudioCacheInvalidator((trackId) => notified.push(trackId));

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );
    unregister();

    expect(notified).toEqual(['t1']);
  });

  it('is a no-op when track_id is missing from the payload', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'pending' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_completed', { audio_ref: 'r' }));
    await settleTrackPatches();

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('pending');
  });

  it('re-downloads a pinned track whose audio the server has just replaced', () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [trackFixture({ id: asTrackId('t1') })]);
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///stale.mp3' } },
      queue: [],
      isWorking: false,
    });

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );

    expect(usePinnedStore.getState().entries.t1?.status).toBe('downloading');
  });

  it('does not start pinning a track the user never downloaded', () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [trackFixture({ id: asTrackId('t1') })]);
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );

    expect(usePinnedStore.getState().entries).toEqual({});
  });
});

describe('track_replace_failed', () => {
  it('reverts to ready, clears the failure state, and keeps the preserved audio_ref', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({
        id: asTrackId('t1'),
        acquisition_status: 'failed',
        failure_reason: 'no_source',
        audio_ref: 'preserved-ref',
      }),
    ]);
    useTrackStatusStore
      .getState()
      .patch(asTrackId('t1'), { acquisitionStatus: 'failed', failureMessage: 'No source found' });

    applyServerEvent(
      queryClient,
      serverEvent('track_replace_failed', { track_id: 't1', reason: 'no_source' }),
    );
    await settleTrackPatches();

    const patched = readTrackPages(queryClient, key).items[0]!;
    expect(patched.acquisition_status).toBe('ready');
    expect(patched.failure_reason).toBeNull();
    expect(patched.audio_ref).toBe('preserved-ref');
    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'ready',
      failureMessage: null,
    });
    expect(useDownloadStore.getState().entries.t1?.phase).toBe('failed');
  });

  it('is a no-op when track_id is missing from the payload', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'failed' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_replace_failed', { reason: 'no_source' }));
    await settleTrackPatches();

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('failed');
  });
});

describe('track_acquisition_failed', () => {
  it('marks the track failed using only the fields the server actually sends', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'pending', audio_ref: 'stale-ref' }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
    );
    await settleTrackPatches();

    const patched = readTrackPages(queryClient, key).items[0]!;
    expect(patched.acquisition_status).toBe('failed');
    expect(patched.failure_reason).toBe('no_candidates');
    expect(patched.audio_ref).toBeNull();
    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'failed',
      failureMessage: null,
    });
    expect(useDownloadStore.getState().entries.t1?.phase).toBe('failed');
  });

  it('preserves an existing failure_message when the event omits one', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({
        id: asTrackId('t1'),
        acquisition_status: 'failed',
        failure_reason: 'no_candidates',
        failure_message: 'No sources matched this recording',
      }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
    );
    await settleTrackPatches();

    expect(readTrackPages(queryClient, key).items[0]!.failure_message).toBe(
      'No sources matched this recording',
    );
  });

  it('keeps only the message the event itself carried in the status store', async () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [
      trackFixture({
        id: asTrackId('t1'),
        acquisition_status: 'failed',
        failure_reason: 'no_candidates',
        failure_message: 'No sources matched this recording',
      }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
    );
    await settleTrackPatches();

    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'failed',
      failureMessage: null,
    });
  });

  it('is a no-op when track_id is missing from the payload', async () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: asTrackId('t1'), acquisition_status: 'pending' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_failed', { reason: 'x' }));
    await settleTrackPatches();

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('pending');
  });
});

describe('playlist_renamed', () => {
  it('renames the playlist in both the detail and list caches', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), playlistDetailFixture());
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [playlistSummaryFixture()],
      total: 1,
    });

    applyServerEvent(
      queryClient,
      serverEvent('playlist_renamed', { playlist_id: 'p1', name: 'New Name' }),
    );

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!
        .name,
    ).toBe('New Name');
    expect(queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.name).toBe(
      'New Name',
    );
  });

  it('accepts an empty string as a valid new name', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), playlistDetailFixture());

    applyServerEvent(queryClient, serverEvent('playlist_renamed', { playlist_id: 'p1', name: '' }));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!
        .name,
    ).toBe('');
  });

  it.each<[string, Record<string, unknown>]>([
    ['name', { playlist_id: 'p1' }],
    ['playlist_id', { name: 'New Name' }],
  ])('leaves the cache untouched when %s is missing', (_label, payload) => {
    const queryClient = makeClient();
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), playlistDetailFixture());

    applyServerEvent(queryClient, serverEvent('playlist_renamed', payload));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!
        .name,
    ).toBe('Old Name');
  });
});

describe('track_removed_from_playlist', () => {
  it('removes the track from the detail cache and decrements the list track_count', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [trackFixture({ id: asTrackId('t1') }), trackFixture({ id: asTrackId('t2') })],
        track_count: 2,
      }),
    );
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [playlistSummaryFixture({ track_count: 2 })],
      total: 1,
    });

    applyServerEvent(
      queryClient,
      serverEvent('track_removed_from_playlist', { playlist_id: 'p1', track_id: 't1' }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2']);
    expect(detail.track_count).toBe(1);
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(1);
  });

  it('replaying the same removal twice never drives track_count negative', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({ tracks: [trackFixture({ id: asTrackId('t1') })], track_count: 1 }),
    );
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [playlistSummaryFixture({ track_count: 1 })],
      total: 1,
    });
    const removal = serverEvent('track_removed_from_playlist', {
      playlist_id: 'p1',
      track_id: 't1',
    });

    applyServerEvent(queryClient, removal);
    applyServerEvent(queryClient, removal);

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.track_count).toBe(0);
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(0);
  });

  it.each<[string, Record<string, unknown>]>([
    ['track_id', { playlist_id: 'p1' }],
    ['playlist_id', { track_id: 't1' }],
  ])('leaves the cache untouched when %s is missing', (_label, payload) => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({ tracks: [trackFixture({ id: asTrackId('t1') })], track_count: 1 }),
    );

    applyServerEvent(queryClient, serverEvent('track_removed_from_playlist', payload));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!
        .track_count,
    ).toBe(1);
  });
});

describe('tracks_removed_from_playlist', () => {
  it('removes every listed track in one pass and lands track_count on the true remainder', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [
          trackFixture({ id: asTrackId('t1') }),
          trackFixture({ id: asTrackId('t2') }),
          trackFixture({ id: asTrackId('t3') }),
        ],
        track_count: 3,
      }),
    );
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [playlistSummaryFixture({ track_count: 3 })],
      total: 1,
    });

    applyServerEvent(
      queryClient,
      serverEvent('tracks_removed_from_playlist', {
        playlist_id: 'p1',
        track_ids: ['t1', 't3'],
      }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2']);
    expect(detail.track_count).toBe(1);
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(1);
  });

  it('replaying the same batch removal twice equals applying it once', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [trackFixture({ id: asTrackId('t1') }), trackFixture({ id: asTrackId('t2') })],
        track_count: 2,
      }),
    );
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [playlistSummaryFixture({ track_count: 2 })],
      total: 1,
    });
    const removal = serverEvent('tracks_removed_from_playlist', {
      playlist_id: 'p1',
      track_ids: ['t1', 't2'],
    });

    applyServerEvent(queryClient, removal);
    applyServerEvent(queryClient, removal);

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks).toEqual([]);
    expect(detail.track_count).toBe(0);
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(0);
  });

  it.each<[string, Record<string, unknown>]>([
    ['track_ids', { playlist_id: 'p1' }],
    ['playlist_id', { track_ids: ['t1'] }],
  ])('leaves the cache untouched when %s is missing', (_label, payload) => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({ tracks: [trackFixture({ id: asTrackId('t1') })], track_count: 1 }),
    );

    applyServerEvent(queryClient, serverEvent('tracks_removed_from_playlist', payload));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!
        .track_count,
    ).toBe(1);
  });

  it('drops non-string entries rather than removing a track named "undefined"', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [trackFixture({ id: asTrackId('t1') }), trackFixture({ id: asTrackId('t2') })],
        track_count: 2,
      }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('tracks_removed_from_playlist', {
        playlist_id: 'p1',
        track_ids: ['t1', 42, null],
      }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2']);
  });
});

describe('playlist_reordered', () => {
  it('reorders tracks to match the given order and appends unlisted tracks at the end', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [
          trackFixture({ id: asTrackId('t1') }),
          trackFixture({ id: asTrackId('t2') }),
          trackFixture({ id: asTrackId('t3') }),
        ],
      }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('playlist_reordered', { playlist_id: 'p1', track_ids: ['t3', 't1'] }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t3', 't1', 't2']);
  });

  it('ignores non-string entries mixed into track_ids', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [trackFixture({ id: asTrackId('t1') }), trackFixture({ id: asTrackId('t2') })],
      }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('playlist_reordered', { playlist_id: 'p1', track_ids: ['t2', 123, null, 't1'] }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2', 't1']);
  });

  it('leaves the order untouched when track_ids is not an array', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [trackFixture({ id: asTrackId('t1') }), trackFixture({ id: asTrackId('t2') })],
      }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('playlist_reordered', { playlist_id: 'p1', track_ids: 'not-an-array' }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t1', 't2']);
  });

  it('leaves the order untouched when playlist_id is missing', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      playlistDetailFixture({
        tracks: [trackFixture({ id: asTrackId('t1') }), trackFixture({ id: asTrackId('t2') })],
      }),
    );

    applyServerEvent(queryClient, serverEvent('playlist_reordered', { track_ids: ['t2', 't1'] }));

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t1', 't2']);
  });
});

describe('invalidate-only events', () => {
  it.each<[string, (readonly string[])[]]>([
    ['playlist_created', [playlistKeys.list]],
    ['playlist_deleted', [playlistKeys.list, playlistKeys.details]],
    ['track_added_to_playlist', [playlistKeys.details, playlistKeys.list]],
    ['tracks_added_to_playlist', [playlistKeys.details, playlistKeys.list]],
  ])('invalidates exactly the mapped keys for %s', (type, expectedKeys) => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    applyServerEvent(queryClient, serverEvent(type, { playlist_id: 'p1' }));

    expect(invalidatedKeys(spy)).toEqual(expectedKeys);
  });
});

describe('downloads bar title', () => {
  function addedToLibraryEvent(overrides: Record<string, unknown> = {}): ServerEvent {
    return serverEvent('track_added_to_library', {
      id: 't1',
      title: 'Song Title',
      artist: 'The Artist',
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'pending',
      album: null,
      duration_seconds: null,
      artwork_url: 'https://cdn/art.png',
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      ...overrides,
    });
  }

  beforeEach(() => {
    useTrackStatusStore.getState().reset();
    useDownloadStore.getState().reset();
  });

  afterEach(() => {
    useDownloadStore.getState().reset();
  });

  describe('added -> started with no library query ever cached', () => {
    it('gives the started entry the real title instead of falling back to null', () => {
      const queryClient = makeClient();

      applyServerEvent(queryClient, addedToLibraryEvent());
      applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

      const entry = useDownloadStore.getState().entries[asTrackId('t1')];
      expect(entry?.title).toBe('Song Title');
      expect(entry?.artist).toBe('The Artist');
      expect(entry?.artworkUrl).toBe('https://cdn/art.png');
    });
  });

  describe('added -> failed (refused save) with no `started` in between', () => {
    it('gives the failed entry the real title', () => {
      const queryClient = makeClient();

      applyServerEvent(queryClient, addedToLibraryEvent());
      applyServerEvent(
        queryClient,
        serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
      );

      const entry = useDownloadStore.getState().entries[asTrackId('t1')];
      expect(entry?.phase).toBe('failed');
      expect(entry?.title).toBe('Song Title');
    });
  });

  describe('added_to_library alone', () => {
    it('does not create a visible download entry', () => {
      const queryClient = makeClient();

      applyServerEvent(queryClient, addedToLibraryEvent());

      expect(useDownloadStore.getState().entries[asTrackId('t1')]).toBeUndefined();
    });
  });

  describe('reset', () => {
    it('clears remembered metadata along with active entries', () => {
      const queryClient = makeClient();

      applyServerEvent(queryClient, addedToLibraryEvent());
      useDownloadStore.getState().reset();
      applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

      expect(useDownloadStore.getState().entries[asTrackId('t1')]?.title).toBeNull();
    });
  });
});

describe('acquisition event bursts at scale', () => {
  // A library the size a real one reaches, all of it cached with staleTime: Infinity, so a
  // pass over it is the 5,000-element scan a bulk import used to pay for on every event.
  const PAGE_COUNT = 25;
  const PAGE_SIZE = 200;
  const LIBRARY_SIZE = PAGE_COUNT * PAGE_SIZE;
  const IMPORTED_TRACKS = 60;

  const LIBRARY_KEY = libraryKeys.tracks('', 'recent');

  type TrackPages = InfiniteData<ListTracksResponse>;
  type RowReads = { count: number };

  function trackFixture(id: string, overrides: Partial<TrackResponse> = {}): TrackResponse {
    return {
      id: asTrackId(id),
      title: `Title ${id}`,
      artist: 'Artist',
      album: null,
      duration_seconds: null,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'pending',
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
      ...overrides,
    } as TrackResponse;
  }

  // Every pass over a cached list reads each row's id once to decide whether the row is the
  // patched one, so an id getter counts array-element visits without touching the subject.
  function countedTrack(id: string, reads: RowReads): TrackResponse {
    return Object.defineProperty(trackFixture(id), 'id', {
      enumerable: true,
      configurable: true,
      get: () => {
        reads.count += 1;
        return id;
      },
    });
  }

  function rowIds(from: number, count: number): string[] {
    return Array.from({ length: count }, (_, i) => `row${from + i}`);
  }

  function listResponse(items: TrackResponse[], offset: number): ListTracksResponse {
    return {
      items,
      total: LIBRARY_SIZE,
      limit: PAGE_SIZE,
      offset,
      has_more: offset + items.length < LIBRARY_SIZE,
    };
  }

  function seedCaches(client: QueryClient, reads: RowReads): void {
    const pages = Array.from({ length: PAGE_COUNT }, (_, page) =>
      listResponse(
        rowIds(page * PAGE_SIZE, PAGE_SIZE).map((id) => countedTrack(id, reads)),
        page * PAGE_SIZE,
      ),
    );
    client.setQueryData<TrackPages>(LIBRARY_KEY, { pages, pageParams: pages.map((_, i) => i) });
    client.setQueryData<ListTracksResponse>(
      libraryKeys.lookup('q'),
      listResponse(
        rowIds(0, PAGE_SIZE).map((id) => countedTrack(id, reads)),
        0,
      ),
    );
    client.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')), {
      id: asPlaylistId('p1'),
      name: 'Imported',
      track_count: PAGE_SIZE,
      preview_artwork_urls: [],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      total_duration_seconds: 0,
      tracks: rowIds(0, PAGE_SIZE).map((id) => countedTrack(id, reads)),
    });
  }

  // The ids the import touches, spread across the cached pages rather than bunched in one.
  const importedIds = Array.from({ length: IMPORTED_TRACKS }, (_, i) => `row${i * 83}`);

  function completedEvent(trackId: string, index: number): ServerEvent {
    return {
      id: String(index),
      type: 'track_acquisition_completed',
      data: { track_id: trackId, audio_ref: `ref-${trackId}` },
    };
  }

  function newClient(): QueryClient {
    return new QueryClient({ defaultOptions: { queries: { retry: false } } });
  }

  function readLibraryRow(client: QueryClient, trackId: string): TrackResponse | undefined {
    return client
      .getQueryData<TrackPages>(LIBRARY_KEY)!
      .pages.flatMap((page) => page.items)
      .find((t) => t.id === trackId);
  }

  beforeEach(() => {
    useTrackStatusStore.getState().reset();
    useDownloadStore.getState().reset();
  });

  describe('a bulk import arriving as one burst of acquisition events', () => {
    it('visits each cached row once for the whole burst rather than once per event', async () => {
      const batchedReads: RowReads = { count: 0 };
      const perEventReads: RowReads = { count: 0 };
      const batched = newClient();
      const perEvent = newClient();
      seedCaches(batched, batchedReads);
      seedCaches(perEvent, perEventReads);

      importedIds.forEach((id, i) => applyServerEvent(batched, completedEvent(id, i)));
      await settleTrackPatches();
      for (const [i, id] of importedIds.entries()) {
        applyServerEvent(perEvent, completedEvent(id, i));
        await settleTrackPatches();
      }

      const [batchedVisits, perEventVisits] = [batchedReads.count, perEventReads.count];
      expect(perEventVisits).toBeGreaterThan(IMPORTED_TRACKS * LIBRARY_SIZE);
      expect(batchedVisits * (IMPORTED_TRACKS / 2)).toBeLessThan(perEventVisits);
      expect(readLibraryRow(batched, 'row83')).toEqual(readLibraryRow(perEvent, 'row83'));
    });

    it('writes the paged library cache once, whatever the number of events in the burst', async () => {
      const client = newClient();
      seedCaches(client, { count: 0 });
      const cache = client.getQueryCache();
      const libraryHash = cache.find({ queryKey: LIBRARY_KEY })!.queryHash;
      let libraryWrites = 0;
      const unsubscribe = cache.subscribe((event) => {
        if (event.type !== 'updated' || event.query.queryHash !== libraryHash) return;
        if (event.action.type === 'success') libraryWrites += 1;
      });

      importedIds.forEach((id, i) => applyServerEvent(client, completedEvent(id, i)));
      await settleTrackPatches();
      unsubscribe();

      expect(libraryWrites).toBe(1);
      expect(readLibraryRow(client, 'row166')?.acquisition_status).toBe('ready');
      expect(readLibraryRow(client, 'row166')?.audio_ref).toBe('ref-row166');
    });
  });

  // The burst above is all completions because those are the events that only patch. The
  // started, progress and failed handlers also read the cache through getTrackFromCaches,
  // which is its own per-event scan and outside this ticket.
  describe('a mixed burst of acquisition events', () => {
    const t1 = 'row1';
    const t2 = 'row2';

    function mixedEvents(): ServerEvent[] {
      return [
        { id: 'a', type: 'track_acquisition_started', data: { track_id: t1 } },
        { id: 'b', type: 'track_acquisition_progress', data: { track_id: t1, stage: 'download' } },
        { id: 'c', type: 'track_replace_failed', data: { track_id: t1, reason: 'no_source' } },
        {
          id: 'd',
          type: 'track_acquisition_failed',
          data: { track_id: t1, reason: 'no_candidates', failure_message: 'nothing matched' },
        },
        // Carries no message of its own, so it keeps the one the event before it cached —
        // a read of a patch that, batched, has not reached the cache yet.
        { id: 'e', type: 'track_acquisition_failed', data: { track_id: t1, reason: 'no_source' } },
        { id: 'f', type: 'track_acquisition_started', data: { track_id: t2 } },
        { id: 'g', type: 'track_acquisition_completed', data: { track_id: t2, audio_ref: 'ref-2' } },
        // A patch for a track no cache holds yet, ahead of the event that adds it.
        {
          id: 'h',
          type: 'track_acquisition_completed',
          data: { track_id: 'row9000', audio_ref: 'ref-9000' },
        },
        { id: 'k', type: 'track_added_to_library', data: trackFixture('row9000') },
        { id: 'i', type: 'track_acquisition_started', data: { track_id: 'row3' } },
        { id: 'j', type: 'track_deleted', data: { track_id: 'row3' } },
      ];
    }

    function seedSmallCaches(client: QueryClient): void {
      const items = rowIds(1, 3).map((id) => trackFixture(id));
      client.setQueryData<TrackPages>(LIBRARY_KEY, {
        pages: [{ items, total: 3, limit: 20, offset: 0, has_more: false }],
        pageParams: [0],
      });
      client.setQueryData<ListTracksResponse>(libraryKeys.lookup('q'), {
        items: rowIds(1, 3).map((id) => trackFixture(id)),
        total: 3,
        limit: 20,
        offset: 0,
        has_more: false,
      });
    }

    it('settles to the cache that applying each event in turn produces', async () => {
      const batched = newClient();
      const perEvent = newClient();
      seedSmallCaches(batched);
      seedSmallCaches(perEvent);

      mixedEvents().forEach((event) => applyServerEvent(batched, event));
      await settleTrackPatches();
      for (const event of mixedEvents()) {
        applyServerEvent(perEvent, event);
        await settleTrackPatches();
      }

      expect(batched.getQueryData<TrackPages>(LIBRARY_KEY)).toEqual(
        perEvent.getQueryData<TrackPages>(LIBRARY_KEY),
      );
      expect(batched.getQueryData(libraryKeys.lookup('q'))).toEqual(
        perEvent.getQueryData(libraryKeys.lookup('q')),
      );
      expect(readLibraryRow(batched, t1)?.failure_message).toBe('nothing matched');
      expect(readLibraryRow(batched, t1)?.failure_reason).toBe('no_source');
      expect(readLibraryRow(batched, t2)?.acquisition_status).toBe('ready');
      expect(readLibraryRow(batched, 'row3')).toBeUndefined();
      expect(readLibraryRow(batched, 'row9000')?.audio_ref).toBeNull();
    });
  });
});
