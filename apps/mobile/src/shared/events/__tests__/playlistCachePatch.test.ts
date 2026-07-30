import { QueryClient } from '@tanstack/react-query';
import fc from 'fast-check';

import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';

import {
  patchPlaylistName,
  removeTrackFromPlaylistCache,
  reorderPlaylistCache,
} from '../playlistCachePatch';

function makeTrack(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: 't1',
    title: 'Track One',
    artist: 'Artist One',
    album: null,
    duration_seconds: 180,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
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

function makePlaylistSummary(overrides: Partial<PlaylistResponse> = {}): PlaylistResponse {
  return {
    id: 'p1',
    name: 'My Playlist',
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  };
}

function makePlaylistDetail(
  id: string,
  tracks: TrackResponse[],
  overrides: Partial<PlaylistDetailResponse> = {},
): PlaylistDetailResponse {
  return {
    id,
    name: 'My Playlist',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
    ...overrides,
  };
}

function makeList(items: PlaylistResponse[], overrides: Partial<ListPlaylistsResponse> = {}): ListPlaylistsResponse {
  return { items, total: items.length, ...overrides };
}

function newClient(): QueryClient {
  return new QueryClient();
}

describe('patchPlaylistName', () => {
  it('renames the cached detail while leaving other fields untouched', () => {
    const client = newClient();
    const detail = makePlaylistDetail('p1', [makeTrack({ id: 'a' })], { name: 'Old Name' });
    client.setQueryData(playlistKeys.detail('p1'), detail);

    patchPlaylistName(client, 'p1', 'New Name');

    expect(client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))).toEqual({
      ...detail,
      name: 'New Name',
    });
  });

  it('renames only the matching entry in the cached list', () => {
    const client = newClient();
    const target = makePlaylistSummary({ id: 'p1', name: 'Old Name' });
    const other = makePlaylistSummary({ id: 'p2', name: 'Other' });
    client.setQueryData(playlistKeys.list, makeList([target, other]));

    patchPlaylistName(client, 'p1', 'New Name');

    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(list.items[0]).toEqual({ ...target, name: 'New Name' });
    expect(list.items[1]).toEqual(other);
  });

  it('keeps the detail and list caches in agreement when both are cached', () => {
    const client = newClient();
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [], { name: 'Old' }));
    client.setQueryData(playlistKeys.list, makeList([makePlaylistSummary({ id: 'p1', name: 'Old' })]));

    patchPlaylistName(client, 'p1', 'Renamed');

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(detail.name).toBe('Renamed');
    expect(list.items[0]!.name).toBe('Renamed');
  });

  it('is a no-op on the detail cache when nothing is cached for that id, but still patches the list', () => {
    const client = newClient();
    client.setQueryData(playlistKeys.list, makeList([makePlaylistSummary({ id: 'p1', name: 'Old' })]));

    patchPlaylistName(client, 'p1', 'New Name');

    expect(client.getQueryData(playlistKeys.detail('p1'))).toBeUndefined();
    expect(client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.name).toBe(
      'New Name',
    );
  });

  it('is a no-op on the list cache when the list is not cached, but still patches the detail', () => {
    const client = newClient();
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [], { name: 'Old' }));

    patchPlaylistName(client, 'p1', 'New Name');

    expect(client.getQueryData(playlistKeys.list)).toBeUndefined();
    expect(client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!.name).toBe(
      'New Name',
    );
  });

  it('is idempotent: renaming twice with the same name equals renaming once', () => {
    const client = newClient();
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [], { name: 'Old' }));

    patchPlaylistName(client, 'p1', 'New Name');
    const afterFirst = client.getQueryData(playlistKeys.detail('p1'));

    patchPlaylistName(client, 'p1', 'New Name');
    const afterSecond = client.getQueryData(playlistKeys.detail('p1'));

    expect(afterSecond).toEqual(afterFirst);
  });
});

describe('removeTrackFromPlaylistCache', () => {
  it('removes the track from the detail cache and recomputes track_count from the remaining tracks', () => {
    const client = newClient();
    const tracks = [makeTrack({ id: 'a' }), makeTrack({ id: 'target' }), makeTrack({ id: 'c' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', tracks));

    removeTrackFromPlaylistCache(client, 'p1', 'target');

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'c']);
    expect(detail.track_count).toBe(2);
  });

  it('decrements only the matching entry in the cached list', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.list,
      makeList([
        makePlaylistSummary({ id: 'p1', track_count: 3 }),
        makePlaylistSummary({ id: 'p2', track_count: 5 }),
      ]),
    );

    removeTrackFromPlaylistCache(client, 'p1', 'target');

    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(list.items[0]!.track_count).toBe(2);
    expect(list.items[1]!.track_count).toBe(5);
  });

  it('never drops the list track_count below zero', () => {
    const client = newClient();
    client.setQueryData(playlistKeys.list, makeList([makePlaylistSummary({ id: 'p1', track_count: 0 })]));

    removeTrackFromPlaylistCache(client, 'p1', 'target');

    expect(client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count).toBe(0);
  });

  it('leaves the detail cache unchanged when the track was never in it', () => {
    const client = newClient();
    const tracks = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', tracks));

    removeTrackFromPlaylistCache(client, 'p1', 'not-in-playlist');

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'b']);
    expect(detail.track_count).toBe(2);
  });

  it('is a no-op on the detail cache when nothing is cached for that playlist id', () => {
    const client = newClient();
    expect(() => removeTrackFromPlaylistCache(client, 'unknown-playlist', 'target')).not.toThrow();
    expect(client.getQueryData(playlistKeys.detail('unknown-playlist'))).toBeUndefined();
  });

  it('is a no-op on the list cache when the list is not cached', () => {
    const client = newClient();
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [makeTrack({ id: 'target' })]));

    expect(() => removeTrackFromPlaylistCache(client, 'p1', 'target')).not.toThrow();
    expect(client.getQueryData(playlistKeys.list)).toBeUndefined();
  });

  it('keeps the list track_count in agreement with the detail track_count when the removal is redelivered', () => {
    const client = newClient();
    const tracks = [makeTrack({ id: 'a' }), makeTrack({ id: 'target' }), makeTrack({ id: 'c' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', tracks, { track_count: 3 }));
    client.setQueryData(playlistKeys.list, makeList([makePlaylistSummary({ id: 'p1', track_count: 3 })]));

    removeTrackFromPlaylistCache(client, 'p1', 'target');
    removeTrackFromPlaylistCache(client, 'p1', 'target');

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(list.items[0]!.track_count).toBe(detail.track_count);
  });
});

describe('reorderPlaylistCache', () => {
  it('reorders cached tracks to match the given id sequence', () => {
    const client = newClient();
    const [a, b, c] = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' }), makeTrack({ id: 'c' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [a, b, c]));

    reorderPlaylistCache(client, 'p1', ['c', 'a', 'b']);

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['c', 'a', 'b']);
  });

  it('appends tracks not named in the sequence after the named ones, preserving their original relative order', () => {
    const client = newClient();
    const [a, b, c, d] = [
      makeTrack({ id: 'a' }),
      makeTrack({ id: 'b' }),
      makeTrack({ id: 'c' }),
      makeTrack({ id: 'd' }),
    ];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [a, b, c, d]));

    reorderPlaylistCache(client, 'p1', ['c', 'a']);

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['c', 'a', 'b', 'd']);
  });

  it('drops sequence ids that are not in the cache without inserting placeholders', () => {
    const client = newClient();
    const [a, b] = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [a, b]));

    reorderPlaylistCache(client, 'p1', ['x', 'a', 'y', 'b']);

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'b']);
  });

  it('preserves the original order when the sequence is empty', () => {
    const client = newClient();
    const [a, b] = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [a, b]));

    reorderPlaylistCache(client, 'p1', []);

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'b']);
  });

  it('is a no-op when nothing is cached for that playlist id', () => {
    const client = newClient();
    expect(() => reorderPlaylistCache(client, 'unknown-playlist', ['a', 'b'])).not.toThrow();
    expect(client.getQueryData(playlistKeys.detail('unknown-playlist'))).toBeUndefined();
  });

  it('is idempotent: reordering twice with the same sequence yields the same order', () => {
    const client = newClient();
    const [a, b, c] = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' }), makeTrack({ id: 'c' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [a, b, c]));

    reorderPlaylistCache(client, 'p1', ['c', 'a', 'b']);
    const afterFirst = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'));

    reorderPlaylistCache(client, 'p1', ['c', 'a', 'b']);
    const afterSecond = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'));

    expect(afterSecond).toEqual(afterFirst);
  });

  it('keeps every cached track exactly once even when the sequence names an id twice', () => {
    const client = newClient();
    const [a, b] = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' })];
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [a, b]));

    reorderPlaylistCache(client, 'p1', ['a', 'a', 'b']);

    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(detail.tracks.map((t) => t.id).sort()).toEqual(['a', 'b']);
  });

  it('property: reordering by any subset/permutation of cached ids preserves the full track membership', () => {
    const ids = ['a', 'b', 'c', 'd', 'e'];
    fc.assert(
      fc.property(fc.shuffledSubarray(ids), (sequence) => {
        const client = newClient();
        const tracks = ids.map((id) => makeTrack({ id }));
        client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', tracks));

        reorderPlaylistCache(client, 'p1', sequence);

        const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
        expect(detail.tracks.map((t) => t.id).sort()).toEqual(ids.slice().sort());
        expect(detail.tracks.map((t) => t.id).slice(0, sequence.length)).toEqual(sequence);
      }),
    );
  });
});
