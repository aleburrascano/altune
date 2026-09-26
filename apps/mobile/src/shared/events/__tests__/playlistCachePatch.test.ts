import { QueryClient, type InfiniteData } from '@tanstack/react-query';
import fc from 'fast-check';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
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
    id: asTrackId('t1'),
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
  } as TrackResponse;
}

function makePlaylistSummary(overrides: Partial<PlaylistResponse> = {}): PlaylistResponse {
  return {
    id: asPlaylistId('p1'),
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
    id: asPlaylistId(id),
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

function makeList(
  items: PlaylistResponse[],
  overrides: Partial<ListPlaylistsResponse> = {},
): ListPlaylistsResponse {
  return { items, total: items.length, ...overrides };
}

function makePages(pages: PlaylistResponse[][]): InfiniteData<ListPlaylistsResponse, number> {
  return {
    pages: pages.map((items) => makeList(items)),
    pageParams: pages.map((_unused, i) => i * 50),
  };
}

function pagedPlaylists(client: QueryClient): InfiniteData<ListPlaylistsResponse, number> {
  return client.getQueryData<InfiniteData<ListPlaylistsResponse, number>>(playlistKeys.paged)!;
}

function newClient(): QueryClient {
  return new QueryClient();
}

describe('patchPlaylistName', () => {
  it('renames the cached detail while leaving other fields untouched', () => {
    const client = newClient();
    const detail = makePlaylistDetail('p1', [makeTrack({ id: asTrackId('a') })], {
      name: 'Old Name',
    });
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), detail);

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');

    expect(
      client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1'))),
    ).toEqual({
      ...detail,
      name: 'New Name',
    });
  });

  it('renames only the matching entry in the cached list', () => {
    const client = newClient();
    const target = makePlaylistSummary({ id: asPlaylistId('p1'), name: 'Old Name' });
    const other = makePlaylistSummary({ id: asPlaylistId('p2'), name: 'Other' });
    client.setQueryData(playlistKeys.list, makeList([target, other]));

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');

    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(list.items[0]).toEqual({ ...target, name: 'New Name' });
    expect(list.items[1]).toEqual(other);
  });

  it('keeps the detail and list caches in agreement when both are cached', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [], { name: 'Old' }),
    );
    client.setQueryData(
      playlistKeys.list,
      makeList([makePlaylistSummary({ id: asPlaylistId('p1'), name: 'Old' })]),
    );

    patchPlaylistName(client, asPlaylistId('p1'), 'Renamed');

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(detail.name).toBe('Renamed');
    expect(list.items[0]!.name).toBe('Renamed');
  });

  it('is a no-op on the detail cache when nothing is cached for that id, but still patches the list', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.list,
      makeList([makePlaylistSummary({ id: asPlaylistId('p1'), name: 'Old' })]),
    );

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');

    expect(client.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toBeUndefined();
    expect(client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.name).toBe(
      'New Name',
    );
  });

  it('is a no-op on the list cache when the list is not cached, but still patches the detail', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [], { name: 'Old' }),
    );

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');

    expect(client.getQueryData(playlistKeys.list)).toBeUndefined();
    expect(
      client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!.name,
    ).toBe('New Name');
  });

  it('is idempotent: renaming twice with the same name equals renaming once', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [], { name: 'Old' }),
    );

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');
    const afterFirst = client.getQueryData(playlistKeys.detail(asPlaylistId('p1')));

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');
    const afterSecond = client.getQueryData(playlistKeys.detail(asPlaylistId('p1')));

    expect(afterSecond).toEqual(afterFirst);
  });
});

describe('removeTrackFromPlaylistCache', () => {
  it('removes the track from the detail cache and recomputes track_count from the remaining tracks', () => {
    const client = newClient();
    const tracks = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('target') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makePlaylistDetail('p1', tracks));

    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target'));

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'c']);
    expect(detail.track_count).toBe(2);
  });

  it('decrements only the matching entry in the cached list', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.list,
      makeList([
        makePlaylistSummary({ id: asPlaylistId('p1'), track_count: 3 }),
        makePlaylistSummary({ id: asPlaylistId('p2'), track_count: 5 }),
      ]),
    );

    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target'));

    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(list.items[0]!.track_count).toBe(2);
    expect(list.items[1]!.track_count).toBe(5);
  });

  it('never drops the list track_count below zero', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.list,
      makeList([makePlaylistSummary({ id: asPlaylistId('p1'), track_count: 0 })]),
    );

    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target'));

    expect(
      client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(0);
  });

  it('leaves the detail cache unchanged when the track was never in it', () => {
    const client = newClient();
    const tracks = [makeTrack({ id: asTrackId('a') }), makeTrack({ id: asTrackId('b') })];
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makePlaylistDetail('p1', tracks));

    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('not-in-playlist'));

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'b']);
    expect(detail.track_count).toBe(2);
  });

  it('is a no-op on the detail cache when nothing is cached for that playlist id', () => {
    const client = newClient();
    expect(() =>
      removeTrackFromPlaylistCache(client, asPlaylistId('unknown-playlist'), asTrackId('target')),
    ).not.toThrow();
    expect(
      client.getQueryData(playlistKeys.detail(asPlaylistId('unknown-playlist'))),
    ).toBeUndefined();
  });

  it('is a no-op on the list cache when the list is not cached', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [makeTrack({ id: asTrackId('target') })]),
    );

    expect(() =>
      removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target')),
    ).not.toThrow();
    expect(client.getQueryData(playlistKeys.list)).toBeUndefined();
  });

  it('keeps the list track_count in agreement with the detail track_count when the removal is redelivered', () => {
    const client = newClient();
    const tracks = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('target') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', tracks, { track_count: 3 }),
    );
    client.setQueryData(
      playlistKeys.list,
      makeList([makePlaylistSummary({ id: asPlaylistId('p1'), track_count: 3 })]),
    );

    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target'));
    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target'));

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    const list = client.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(list.items[0]!.track_count).toBe(detail.track_count);
  });
});

describe('reorderPlaylistCache', () => {
  it('reorders cached tracks to match the given id sequence', () => {
    const client = newClient();
    const [a, b, c] = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('b') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [a, b, c]),
    );

    reorderPlaylistCache(client, asPlaylistId('p1'), ['c', 'a', 'b']);

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['c', 'a', 'b']);
  });

  it('appends tracks not named in the sequence after the named ones, preserving their original relative order', () => {
    const client = newClient();
    const [a, b, c, d] = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('b') }),
      makeTrack({ id: asTrackId('c') }),
      makeTrack({ id: asTrackId('d') }),
    ];
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [a, b, c, d]),
    );

    reorderPlaylistCache(client, asPlaylistId('p1'), ['c', 'a']);

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['c', 'a', 'b', 'd']);
  });

  it('drops sequence ids that are not in the cache without inserting placeholders', () => {
    const client = newClient();
    const [a, b] = [makeTrack({ id: asTrackId('a') }), makeTrack({ id: asTrackId('b') })];
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makePlaylistDetail('p1', [a, b]));

    reorderPlaylistCache(client, asPlaylistId('p1'), ['x', 'a', 'y', 'b']);

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'b']);
  });

  it('preserves the original order when the sequence is empty', () => {
    const client = newClient();
    const [a, b] = [makeTrack({ id: asTrackId('a') }), makeTrack({ id: asTrackId('b') })];
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makePlaylistDetail('p1', [a, b]));

    reorderPlaylistCache(client, asPlaylistId('p1'), []);

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id)).toEqual(['a', 'b']);
  });

  it('is a no-op when nothing is cached for that playlist id', () => {
    const client = newClient();
    expect(() =>
      reorderPlaylistCache(client, asPlaylistId('unknown-playlist'), ['a', 'b']),
    ).not.toThrow();
    expect(
      client.getQueryData(playlistKeys.detail(asPlaylistId('unknown-playlist'))),
    ).toBeUndefined();
  });

  it('is idempotent: reordering twice with the same sequence yields the same order', () => {
    const client = newClient();
    const [a, b, c] = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('b') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [a, b, c]),
    );

    reorderPlaylistCache(client, asPlaylistId('p1'), ['c', 'a', 'b']);
    const afterFirst = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    );

    reorderPlaylistCache(client, asPlaylistId('p1'), ['c', 'a', 'b']);
    const afterSecond = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    );

    expect(afterSecond).toEqual(afterFirst);
  });

  it('keeps every cached track exactly once even when the sequence names an id twice', () => {
    const client = newClient();
    const [a, b] = [makeTrack({ id: asTrackId('a') }), makeTrack({ id: asTrackId('b') })];
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makePlaylistDetail('p1', [a, b]));

    reorderPlaylistCache(client, asPlaylistId('p1'), ['a', 'a', 'b']);

    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(detail.tracks.map((t) => t.id).sort()).toEqual(['a', 'b']);
  });

  it('property: reordering by any subset/permutation of cached ids preserves the full track membership', () => {
    const ids = ['a', 'b', 'c', 'd', 'e'];
    fc.assert(
      fc.property(fc.shuffledSubarray(ids), (sequence) => {
        const client = newClient();
        const tracks = ids.map((id) => makeTrack({ id: asTrackId(id) }));
        client.setQueryData(
          playlistKeys.detail(asPlaylistId('p1')),
          makePlaylistDetail('p1', tracks),
        );

        reorderPlaylistCache(client, asPlaylistId('p1'), sequence);

        const detail = client.getQueryData<PlaylistDetailResponse>(
          playlistKeys.detail(asPlaylistId('p1')),
        )!;
        expect(detail.tracks.map((t) => t.id).sort()).toEqual(ids.slice().sort());
        expect(detail.tracks.map((t) => t.id).slice(0, sequence.length)).toEqual(sequence);
      }),
    );
  });
});

// The library grid walks the collection a page at a time under its own key (#1708), so a
// patch reaching only the single-response cache would leave the grid showing a stale name
// or count until something else invalidated it.
describe('the library grid pages', () => {
  it('carries the new name after a rename, whichever page the playlist landed on', () => {
    const client = newClient();
    const target = makePlaylistSummary({ id: asPlaylistId('p1'), name: 'Old Name' });
    const other = makePlaylistSummary({ id: asPlaylistId('p2'), name: 'Other' });
    client.setQueryData(playlistKeys.paged, makePages([[other], [target]]));

    patchPlaylistName(client, asPlaylistId('p1'), 'New Name');

    const paged = pagedPlaylists(client);
    expect(paged.pages[1]!.items[0]).toEqual({ ...target, name: 'New Name' });
    expect(paged.pages[0]!.items[0]).toEqual(other);
  });

  it('carries one track fewer after a track is removed from a playlist', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.paged,
      makePages([[makePlaylistSummary({ id: asPlaylistId('p1'), track_count: 3 })]]),
    );

    removeTrackFromPlaylistCache(client, asPlaylistId('p1'), asTrackId('target'));

    expect(pagedPlaylists(client).pages[0]!.items[0]!.track_count).toBe(2);
  });

  it('are left alone when the grid has never been opened', () => {
    const client = newClient();

    expect(() => patchPlaylistName(client, asPlaylistId('p1'), 'New Name')).not.toThrow();
    expect(client.getQueryData(playlistKeys.paged)).toBeUndefined();
  });
});

describe('playlist id branding', () => {
  // Compile-time guard: tsc fails if these cache writers start accepting a bare string again,
  // which is what let an unparsed SSE id choose the detail key a rename wrote to.
  it('refuses a bare string where a PlaylistId belongs', () => {
    const client = newClient();
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [], { name: 'Old' }),
    );

    // @ts-expect-error a raw string must go through asPlaylistId / parsePlaylistId first
    patchPlaylistName(client, 'p1', 'New Name');

    expect(
      client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!.name,
    ).toBe('New Name');
  });
});

describe('scale', () => {
  // The two playlist sizes a reorder is timed at: a short one and an "everything" playlist,
  // sixteen times longer. A patch that scans the named ids once per cached track measures five
  // to ten times more per track on the long one; one that looks them up measures about the same.
  const SHORT_PLAYLIST = 1000;
  const LONG_PLAYLIST = 16000;

  // Allocation and the runner's scheduler add noise on top of the per-track work; three times
  // leaves room for both while still failing anything that walks the id list per track.
  const TOLERATED_GROWTH = 3;

  function makeTrack(id: string): TrackResponse {
    return {
      id: asTrackId(id),
      title: `Track ${id}`,
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
    } as TrackResponse;
  }

  function makeTracks(count: number): TrackResponse[] {
    return Array.from({ length: count }, (_unused, i) => makeTrack(`t${i}`));
  }

  function seedPlaylist(queryClient: QueryClient, tracks: TrackResponse[]): void {
    queryClient.setQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')), {
      id: asPlaylistId('p1'),
      name: 'Everything',
      track_count: tracks.length,
      preview_artwork_urls: [],
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
      total_duration_seconds: 0,
      tracks,
    });
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, {
      items: [
        {
          id: asPlaylistId('p1'),
          name: 'Everything',
          track_count: tracks.length,
          preview_artwork_urls: [],
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ],
      total: 1,
    });
  }

  function cachedDetail(queryClient: QueryClient): PlaylistDetailResponse {
    return queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!;
  }

  // jsdom rounds performance.now() to whole milliseconds, which is coarser than a reorder of a
  // short playlist takes; the process clock is what can tell one from sixteen.
  function nowMs(): number {
    return Number(process.hrtime.bigint()) / 1e6;
  }

  // Milliseconds per track to reverse a playlist of `size` tracks, the reorder a drag from the
  // top to the bottom of the list produces.
  function msPerTrack(size: number): number {
    const queryClient = new QueryClient();
    const tracks = makeTracks(size);
    seedPlaylist(queryClient, tracks);
    const reversedIds = tracks.map((t) => t.id).reverse();

    const startedAt = nowMs();
    reorderPlaylistCache(queryClient, asPlaylistId('p1'), reversedIds);

    return (nowMs() - startedAt) / size;
  }

  // The fastest of three runs: a slow run can only come from noise, never from the patch being
  // cheaper than it is, so the minimum is the measurement least able to flake. The discarded
  // first run is what stops a cold JIT from inflating whichever size is measured first.
  function fastestMsPerTrack(size: number): number {
    msPerTrack(size);
    return Math.min(msPerTrack(size), msPerTrack(size), msPerTrack(size));
  }

  describe('reordering a whole playlist from a playlist_reordered event', () => {
    it('costs the same per track on a long playlist as on a short one, so the patch stays linear in its length', () => {
      const onShort = fastestMsPerTrack(SHORT_PLAYLIST);

      const onLong = fastestMsPerTrack(LONG_PLAYLIST);

      expect(onLong).toBeLessThanOrEqual(onShort * TOLERATED_GROWTH);
    });

    it('lands every track in the order the event named, so the cheap patch still reorders', () => {
      const queryClient = new QueryClient();
      const tracks = makeTracks(LONG_PLAYLIST);
      seedPlaylist(queryClient, tracks);

      reorderPlaylistCache(queryClient, asPlaylistId('p1'), tracks.map((t) => t.id).reverse());

      const reordered = cachedDetail(queryClient).tracks;
      expect(reordered).toHaveLength(LONG_PLAYLIST);
      expect(reordered[0]!.id).toBe(`t${LONG_PLAYLIST - 1}`);
      expect(reordered[LONG_PLAYLIST - 1]!.id).toBe('t0');
    });
  });
});
