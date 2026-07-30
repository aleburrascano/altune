import { QueryClient, type InfiniteData } from '@tanstack/react-query';

import {
  registerAudioCacheInvalidator,
  _resetAudioCacheInvalidatorsForTest,
} from '@shared/acquisition/audioCacheInvalidation';
import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { trackIdentityKey, useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
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
import { _resetUnhandledEventsForTest, unhandledEventTypes } from '../eventTypes';
import type { ServerEvent } from '../sse-client';

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
    id: 't1',
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
  };
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
    id: 'p1',
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
    id: 'p1',
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
  _resetUnhandledEventsForTest();
});

afterEach(() => {
  useDownloadStore.getState().reset();
});

describe('unrecognized event type', () => {
  it('records it instead of throwing or invalidating anything', () => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    expect(() =>
      applyServerEvent(queryClient, serverEvent('track_favourited', { track_id: 't1' })),
    ).not.toThrow();

    expect(unhandledEventTypes()).toEqual(['track_favourited']);
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
        id: 't1',
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
      trackFixture({ id: 't1', title: 'Song Title', artist: 'The Artist' }),
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
    const key = seedTrackPages(queryClient, [trackFixture({ id: 't1' })], 1);
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({ tracks: [trackFixture({ id: 't1' })], track_count: 1 }),
    );
    useTrackStatusStore.getState().patch('t1', { acquisitionStatus: 'ready', failureMessage: null });

    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    applyServerEvent(queryClient, serverEvent('track_deleted', { track_id: 't1' }));

    const page = readTrackPages(queryClient, key);
    expect(page.items).toHaveLength(0);
    expect(page.total).toBe(0);
    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!.tracks,
    ).toHaveLength(0);
    expect(useTrackStatusStore.getState().statuses.t1).toBeUndefined();
    expect(invalidatedKeys(spy)).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
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
      playlistKeys.list,
    ]);
  });

  it('replaying the same deletion twice leaves the cache empty rather than erroring', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [trackFixture({ id: 't1' })], 1);

    applyServerEvent(queryClient, serverEvent('track_deleted', { track_id: 't1' }));
    applyServerEvent(queryClient, serverEvent('track_deleted', { track_id: 't1' }));

    const page = readTrackPages(queryClient, key);
    expect(page.items).toHaveLength(0);
    expect(page.total).toBe(0);
  });
});

describe('track_acquisition_started', () => {
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
        id: 't1',
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

  it('clears a prior failure and reverts the cached track to pending', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'failed', failure_reason: 'no_source' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_started', { track_id: 't1' }));

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
  it('marks the track ready with the new audio_ref and finishes the download', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'pending', audio_ref: null }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );

    const patched = readTrackPages(queryClient, key).items[0]!;
    expect(patched.acquisition_status).toBe('ready');
    expect(patched.audio_ref).toBe('ref-123');
    expect(useTrackStatusStore.getState().statuses.t1).toEqual({
      acquisitionStatus: 'ready',
      failureMessage: null,
    });
    expect(useDownloadStore.getState().entries.t1?.phase).toBe('finishing');
  });

  it('keeps a previously-set audio_ref when a thin completion event omits it', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'pending', audio_ref: 'old-ref' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_completed', { track_id: 't1' }));

    const track = readTrackPages(queryClient, key).items[0]!;
    expect(track.acquisition_status).toBe('ready');
    expect(track.audio_ref).toBe('old-ref');
  });

  it('notifies registered audio cache invalidators with the completed trackId', () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [trackFixture({ id: 't1' })]);
    const notified: string[] = [];
    const unregister = registerAudioCacheInvalidator((trackId) => notified.push(trackId));

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );
    unregister();

    expect(notified).toEqual(['t1']);
  });

  it('is a no-op when track_id is missing from the payload', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'pending' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_completed', { audio_ref: 'r' }));

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('pending');
  });

  it('re-downloads a pinned track whose audio the server has just replaced', () => {
    const queryClient = makeClient();
    seedTrackPages(queryClient, [trackFixture({ id: 't1' })]);
    usePinnedStore.setState({
      entries: { t1: { trackId: 't1', status: 'ready', uri: 'file:///stale.mp3' } },
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
    seedTrackPages(queryClient, [trackFixture({ id: 't1' })]);
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_completed', { track_id: 't1', audio_ref: 'ref-123' }),
    );

    expect(usePinnedStore.getState().entries).toEqual({});
  });
});

describe('track_replace_failed', () => {
  it('reverts to ready, clears the failure state, and keeps the preserved audio_ref', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({
        id: 't1',
        acquisition_status: 'failed',
        failure_reason: 'no_source',
        audio_ref: 'preserved-ref',
      }),
    ]);
    useTrackStatusStore
      .getState()
      .patch('t1', { acquisitionStatus: 'failed', failureMessage: 'No source found' });

    applyServerEvent(
      queryClient,
      serverEvent('track_replace_failed', { track_id: 't1', reason: 'no_source' }),
    );

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

  it('is a no-op when track_id is missing from the payload', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'failed' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_replace_failed', { reason: 'no_source' }));

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('failed');
  });
});

describe('track_acquisition_failed', () => {
  it('marks the track failed using only the fields the server actually sends', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'pending', audio_ref: 'stale-ref' }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
    );

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

  it('preserves an existing failure_message when the event omits one', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({
        id: 't1',
        acquisition_status: 'pending',
        failure_message: 'No sources matched this recording',
      }),
    ]);

    applyServerEvent(
      queryClient,
      serverEvent('track_acquisition_failed', { track_id: 't1', reason: 'no_candidates' }),
    );

    expect(readTrackPages(queryClient, key).items[0]!.failure_message).toBe(
      'No sources matched this recording',
    );
  });

  it('is a no-op when track_id is missing from the payload', () => {
    const queryClient = makeClient();
    const key = seedTrackPages(queryClient, [
      trackFixture({ id: 't1', acquisition_status: 'pending' }),
    ]);

    applyServerEvent(queryClient, serverEvent('track_acquisition_failed', { reason: 'x' }));

    expect(readTrackPages(queryClient, key).items[0]!.acquisition_status).toBe('pending');
  });
});

describe('playlist_renamed', () => {
  it('renames the playlist in both the detail and list caches', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(playlistKeys.detail('p1'), playlistDetailFixture());
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [playlistSummaryFixture()],
      total: 1,
    });

    applyServerEvent(
      queryClient,
      serverEvent('playlist_renamed', { playlist_id: 'p1', name: 'New Name' }),
    );

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!.name,
    ).toBe('New Name');
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.name,
    ).toBe('New Name');
  });

  it('accepts an empty string as a valid new name', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(playlistKeys.detail('p1'), playlistDetailFixture());

    applyServerEvent(
      queryClient,
      serverEvent('playlist_renamed', { playlist_id: 'p1', name: '' }),
    );

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!.name,
    ).toBe('');
  });

  it.each<[string, Record<string, unknown>]>([
    ['name', { playlist_id: 'p1' }],
    ['playlist_id', { name: 'New Name' }],
  ])('leaves the cache untouched when %s is missing', (_label, payload) => {
    const queryClient = makeClient();
    queryClient.setQueryData(playlistKeys.detail('p1'), playlistDetailFixture());

    applyServerEvent(queryClient, serverEvent('playlist_renamed', payload));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!.name,
    ).toBe('Old Name');
  });
});

describe('track_removed_from_playlist', () => {
  it('removes the track from the detail cache and decrements the list track_count', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({
        tracks: [trackFixture({ id: 't1' }), trackFixture({ id: 't2' })],
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

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2']);
    expect(detail.track_count).toBe(1);
    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(1);
  });

  it('replaying the same removal twice never drives track_count negative', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({ tracks: [trackFixture({ id: 't1' })], track_count: 1 }),
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

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
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
      playlistKeys.detail('p1'),
      playlistDetailFixture({ tracks: [trackFixture({ id: 't1' })], track_count: 1 }),
    );

    applyServerEvent(queryClient, serverEvent('track_removed_from_playlist', payload));

    expect(
      queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!.track_count,
    ).toBe(1);
  });
});

describe('playlist_reordered', () => {
  it('reorders tracks to match the given order and appends unlisted tracks at the end', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({
        tracks: [trackFixture({ id: 't1' }), trackFixture({ id: 't2' }), trackFixture({ id: 't3' })],
      }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('playlist_reordered', { playlist_id: 'p1', track_ids: ['t3', 't1'] }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t3', 't1', 't2']);
  });

  it('ignores non-string entries mixed into track_ids', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({ tracks: [trackFixture({ id: 't1' }), trackFixture({ id: 't2' })] }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('playlist_reordered', { playlist_id: 'p1', track_ids: ['t2', 123, null, 't1'] }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t2', 't1']);
  });

  it('leaves the order untouched when track_ids is not an array', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({ tracks: [trackFixture({ id: 't1' }), trackFixture({ id: 't2' })] }),
    );

    applyServerEvent(
      queryClient,
      serverEvent('playlist_reordered', { playlist_id: 'p1', track_ids: 'not-an-array' }),
    );

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t1', 't2']);
  });

  it('leaves the order untouched when playlist_id is missing', () => {
    const queryClient = makeClient();
    queryClient.setQueryData(
      playlistKeys.detail('p1'),
      playlistDetailFixture({ tracks: [trackFixture({ id: 't1' }), trackFixture({ id: 't2' })] }),
    );

    applyServerEvent(queryClient, serverEvent('playlist_reordered', { track_ids: ['t2', 't1'] }));

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['t1', 't2']);
  });
});

describe('invalidate-only events', () => {
  it.each<[string, (readonly string[])[]]>([
    ['playlist_created', [playlistKeys.list]],
    ['playlist_deleted', [playlistKeys.list, playlistKeys.details]],
    ['track_added_to_playlist', [playlistKeys.details, playlistKeys.list]],
  ])('invalidates exactly the mapped keys for %s', (type, expectedKeys) => {
    const queryClient = makeClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    applyServerEvent(queryClient, serverEvent(type, { playlist_id: 'p1' }));

    expect(invalidatedKeys(spy)).toEqual(expectedKeys);
  });
});
