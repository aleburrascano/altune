import { QueryClient, type InfiniteData } from '@tanstack/react-query';
import fc from 'fast-check';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import {
  getTrackFromCaches,
  patchTrackInCaches,
  removeTrackFromCaches,
  replaceTrackInCaches,
  upsertTrackInCaches,
} from '../trackCachePatch';

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
  };
}

function makePage(
  items: TrackResponse[],
  overrides: Partial<ListTracksResponse> = {},
): ListTracksResponse {
  return { items, total: items.length, limit: 20, offset: 0, has_more: false, ...overrides };
}

function makeInfinite(pages: ListTracksResponse[]): InfiniteData<ListTracksResponse> {
  return { pages, pageParams: pages.map((_, i) => i) };
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

function newClient(): QueryClient {
  return new QueryClient();
}

// Registers a query in the cache without ever populating it — the state a screen
// is in between mounting a query and its first fetch resolving (state.data is
// undefined). An SSE cache patch can land in exactly this window.
function registerUnfetchedQuery(client: QueryClient, queryKey: readonly unknown[]): void {
  client.getQueryCache().build(client, { queryKey });
}

function seedTracksPrefix(client: QueryClient, pages: ListTracksResponse[]): void {
  client.setQueryData(libraryKeys.tracks('q', 'sort'), makeInfinite(pages));
}

describe('getTrackFromCaches', () => {
  it('returns undefined when nothing is cached', () => {
    const client = newClient();
    expect(getTrackFromCaches(client, 't1')).toBeUndefined();
  });

  it('finds a track on a later page of a paged tracksPrefix cache', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('a') })]), makePage([target])]);

    expect(getTrackFromCaches(client, 'target')).toEqual(target);
  });

  it('prefers the tracksPrefix copy over a stale copy in the lookup cache', () => {
    const client = newClient();
    const fresh = makeTrack({ id: asTrackId('shared'), title: 'Fresh Title' });
    const stale = makeTrack({ id: asTrackId('shared'), title: 'Stale Title' });
    seedTracksPrefix(client, [makePage([fresh])]);
    client.setQueryData(libraryKeys.lookup('q'), makePage([stale]));

    expect(getTrackFromCaches(client, 'shared')?.title).toBe('Fresh Title');
  });

  it('falls back to the lookup cache when the track is not in any paged cache', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('lookup-only') });
    client.setQueryData(libraryKeys.lookup('q'), makePage([target]));

    expect(getTrackFromCaches(client, 'lookup-only')).toEqual(target);
  });

  it('falls back to the featuring cache', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('featuring-only') });
    client.setQueryData(libraryKeys.featuring('identity'), makePage([target]));

    expect(getTrackFromCaches(client, 'featuring-only')).toEqual(target);
  });

  it('falls back to a cached playlist detail when not found elsewhere', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('playlist-only') });
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [target]));

    expect(getTrackFromCaches(client, 'playlist-only')).toEqual(target);
  });

  it('skips a family whose query is registered but not yet fetched (data undefined)', () => {
    const client = newClient();
    registerUnfetchedQuery(client, libraryKeys.tracks('q', 'sort'));
    registerUnfetchedQuery(client, libraryKeys.lookup('q'));
    registerUnfetchedQuery(client, libraryKeys.featuring('identity'));
    registerUnfetchedQuery(client, playlistKeys.detail('p1'));

    expect(getTrackFromCaches(client, 'anything')).toBeUndefined();
  });

  it('returns undefined for an id absent from every populated family', () => {
    const client = newClient();
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('a') })])]);
    client.setQueryData(libraryKeys.lookup('q'), makePage([makeTrack({ id: asTrackId('b') })]));
    client.setQueryData(
      playlistKeys.detail('p1'),
      makePlaylistDetail('p1', [makeTrack({ id: asTrackId('c') })]),
    );

    expect(getTrackFromCaches(client, 'missing')).toBeUndefined();
  });
});

describe('upsertTrackInCaches', () => {
  it('prepends a brand-new track to the first page and increments only that page total', () => {
    const client = newClient();
    const page1 = makePage([makeTrack({ id: asTrackId('a') })], { total: 5 });
    const page2 = makePage([makeTrack({ id: asTrackId('b') })], { total: 3 });
    seedTracksPrefix(client, [page1, page2]);

    const incoming = makeTrack({ id: asTrackId('new') });
    upsertTrackInCaches(client, incoming);

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items.map((t) => t.id)).toEqual(['new', 'a']);
    expect(result.pages[0]!.total).toBe(6);
    expect(result.pages[1]).toEqual(page2);
  });

  it('merges into place on a later page instead of moving or duplicating the track', () => {
    const client = newClient();
    const existing = makeTrack({ id: asTrackId('b'), title: 'Old Title' });
    const page1 = makePage([makeTrack({ id: asTrackId('a') })], { total: 5 });
    const page2 = makePage([existing], { total: 3 });
    seedTracksPrefix(client, [page1, page2]);

    upsertTrackInCaches(client, makeTrack({ id: asTrackId('b'), title: 'New Title' }));

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items.map((t) => t.id)).toEqual(['a']);
    expect(result.pages[1]!.items).toEqual([makeTrack({ id: asTrackId('b'), title: 'New Title' })]);
    expect(result.pages[0]!.total).toBe(5);
    expect(result.pages[1]!.total).toBe(3);
  });

  it('is idempotent for a new track: applying it twice yields one copy and a single increment', () => {
    const client = newClient();
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('a') })], { total: 1 })]);
    const incoming = makeTrack({ id: asTrackId('new') });

    upsertTrackInCaches(client, incoming);
    upsertTrackInCaches(client, incoming);

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items.filter((t) => t.id === 'new')).toHaveLength(1);
    expect(result.pages[0]!.total).toBe(2);
  });

  it('is a silent no-op when the tracksPrefix cache has no pages to prepend into', () => {
    const client = newClient();
    seedTracksPrefix(client, []);

    upsertTrackInCaches(client, makeTrack({ id: asTrackId('new') }));

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages).toEqual([]);
  });

  it('is a safe no-op when the paged query is registered but not yet fetched (data undefined)', () => {
    const client = newClient();
    registerUnfetchedQuery(client, libraryKeys.tracks('q', 'sort'));

    expect(() => upsertTrackInCaches(client, makeTrack({ id: asTrackId('new') }))).not.toThrow();
    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toBeUndefined();
  });

  it('does not propagate to the lookup, featuring, or playlist detail caches', () => {
    const client = newClient();
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('shared'), title: 'Old' })])]);
    const lookupBefore = makePage([makeTrack({ id: asTrackId('shared'), title: 'Old' })]);
    const detailBefore = makePlaylistDetail('p1', [
      makeTrack({ id: asTrackId('shared'), title: 'Old' }),
    ]);
    client.setQueryData(libraryKeys.lookup('q'), lookupBefore);
    client.setQueryData(playlistKeys.detail('p1'), detailBefore);

    upsertTrackInCaches(client, makeTrack({ id: asTrackId('shared'), title: 'New' }));

    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookupBefore);
    expect(client.getQueryData(playlistKeys.detail('p1'))).toEqual(detailBefore);
  });
});

describe('replaceTrackInCaches', () => {
  it('swaps an optimistic id for the real track, preserving its position and total', () => {
    const client = newClient();
    const optimistic = makeTrack({ id: asTrackId('optimistic-1') });
    const other = makeTrack({ id: asTrackId('other') });
    seedTracksPrefix(client, [makePage([optimistic, other])]);

    const real = makeTrack({ id: asTrackId('real-1'), title: 'Server Title' });
    replaceTrackInCaches(client, 'optimistic-1', real);

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items).toEqual([real, other]);
    expect(result.pages[0]!.total).toBe(2);
  });

  it('drops the duplicate and decrements total when the real track is already cached in the same page', () => {
    const client = newClient();
    const optimistic = makeTrack({ id: asTrackId('optimistic-1') });
    const real = makeTrack({ id: asTrackId('real-1') });
    seedTracksPrefix(client, [makePage([optimistic, real], { total: 2 })]);

    replaceTrackInCaches(client, 'optimistic-1', real);

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items).toEqual([real]);
    expect(result.pages[0]!.total).toBe(1);
  });

  it('leaves the cache unchanged when the optimistic id is not present', () => {
    const client = newClient();
    const page = makePage([makeTrack({ id: asTrackId('a') }), makeTrack({ id: asTrackId('b') })]);
    seedTracksPrefix(client, [page]);

    replaceTrackInCaches(client, 'no-such-optimistic-id', makeTrack({ id: asTrackId('real-1') }));

    expect(
      client.getQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort')),
    ).toEqual(makeInfinite([page]));
  });

  it('is idempotent: replaying the same replacement leaves the cache exactly as the first application did', () => {
    const client = newClient();
    const optimistic = makeTrack({ id: asTrackId('optimistic-1') });
    const other = makeTrack({ id: asTrackId('other') });
    seedTracksPrefix(client, [makePage([optimistic, other])]);
    const real = makeTrack({ id: asTrackId('real-1') });

    replaceTrackInCaches(client, 'optimistic-1', real);
    const afterFirst = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    replaceTrackInCaches(client, 'optimistic-1', real);
    const afterSecond = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    expect(afterSecond).toEqual(afterFirst);
  });
});

describe('removeTrackFromCaches', () => {
  it('removes the track from its page and decrements only that page total', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    const page1 = makePage([makeTrack({ id: asTrackId('a') })], { total: 5 });
    const page2 = makePage([target, makeTrack({ id: asTrackId('b') })], { total: 8 });
    seedTracksPrefix(client, [page1, page2]);

    removeTrackFromCaches(client, 'target');

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]).toEqual(page1);
    expect(result.pages[1]!.items.map((t) => t.id)).toEqual(['b']);
    expect(result.pages[1]!.total).toBe(7);
  });

  it('removes the track from the lookup and featuring flat lists and decrements their totals', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    client.setQueryData(
      libraryKeys.lookup('q'),
      makePage([target, makeTrack({ id: asTrackId('a') })], { total: 9 }),
    );
    client.setQueryData(
      libraryKeys.featuring('identity'),
      makePage([makeTrack({ id: asTrackId('b') }), target], { total: 4 }),
    );

    removeTrackFromCaches(client, 'target');

    const lookup = client.getQueryData<ListTracksResponse>(libraryKeys.lookup('q'))!;
    const featuring = client.getQueryData<ListTracksResponse>(libraryKeys.featuring('identity'))!;
    expect(lookup.items.map((t) => t.id)).toEqual(['a']);
    expect(lookup.total).toBe(8);
    expect(featuring.items.map((t) => t.id)).toEqual(['b']);
    expect(featuring.total).toBe(3);
  });

  it('removes the track from every cached playlist detail regardless of playlist', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    client.setQueryData(
      playlistKeys.detail('p1'),
      makePlaylistDetail('p1', [target, makeTrack({ id: asTrackId('a') })]),
    );
    client.setQueryData(
      playlistKeys.detail('p2'),
      makePlaylistDetail('p2', [makeTrack({ id: asTrackId('b') }), target]),
    );

    removeTrackFromCaches(client, 'target');

    const p1 = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    const p2 = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p2'))!;
    expect(p1.tracks.map((t) => t.id)).toEqual(['a']);
    expect(p2.tracks.map((t) => t.id)).toEqual(['b']);
  });

  it('keeps a playlist detail track_count consistent with its tracks after the removal', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    client.setQueryData(
      playlistKeys.detail('p1'),
      makePlaylistDetail('p1', [target, makeTrack({ id: asTrackId('a') })], { track_count: 2 }),
    );

    removeTrackFromCaches(client, 'target');

    const p1 = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(p1.track_count).toBe(p1.tracks.length);
  });

  it('is idempotent: removing an already-removed track a second time makes no further change', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    seedTracksPrefix(client, [makePage([target, makeTrack({ id: asTrackId('a') })], { total: 6 })]);

    removeTrackFromCaches(client, 'target');
    const afterFirst = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    removeTrackFromCaches(client, 'target');
    const afterSecond = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    expect(afterSecond).toEqual(afterFirst);
  });

  it('is a no-op across every family when the id is not cached anywhere', () => {
    const client = newClient();
    const page = makePage([makeTrack({ id: asTrackId('a') })]);
    const lookup = makePage([makeTrack({ id: asTrackId('b') })]);
    const detail = makePlaylistDetail('p1', [makeTrack({ id: asTrackId('c') })]);
    seedTracksPrefix(client, [page]);
    client.setQueryData(libraryKeys.lookup('q'), lookup);
    client.setQueryData(playlistKeys.detail('p1'), detail);

    removeTrackFromCaches(client, 'not-cached');

    expect(
      client.getQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort')),
    ).toEqual(makeInfinite([page]));
    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookup);
    expect(client.getQueryData(playlistKeys.detail('p1'))).toEqual(detail);
  });
});

describe('patchTrackInCaches', () => {
  it('applies a partial patch to the matching track in a page, preserving unrelated fields and total', () => {
    const client = newClient();
    const target = makeTrack({
      id: asTrackId('target'),
      acquisition_status: 'pending',
      title: 'Original',
    });
    const other = makeTrack({ id: asTrackId('other') });
    seedTracksPrefix(client, [makePage([target, other], { total: 9 })]);

    patchTrackInCaches(client, 'target', { acquisition_status: 'ready' });

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items[0]).toEqual({
      ...target,
      acquisition_status: 'ready',
    });
    expect(result.pages[0]!.items[1]).toEqual(other);
    expect(result.pages[0]!.total).toBe(9);
  });

  it('propagates an identical patch to every family caching a copy of the track', () => {
    const client = newClient();
    const inPage = makeTrack({
      id: asTrackId('shared'),
      acquisition_status: 'pending',
      title: 'Page Copy',
    });
    const inLookup = makeTrack({
      id: asTrackId('shared'),
      acquisition_status: 'pending',
      title: 'Lookup Copy',
    });
    const inFeaturing = makeTrack({
      id: asTrackId('shared'),
      acquisition_status: 'pending',
      title: 'Featuring Copy',
    });
    const inDetail = makeTrack({
      id: asTrackId('shared'),
      acquisition_status: 'pending',
      title: 'Detail Copy',
    });
    seedTracksPrefix(client, [makePage([inPage])]);
    client.setQueryData(libraryKeys.lookup('q'), makePage([inLookup]));
    client.setQueryData(libraryKeys.featuring('identity'), makePage([inFeaturing]));
    client.setQueryData(playlistKeys.detail('p1'), makePlaylistDetail('p1', [inDetail]));

    patchTrackInCaches(client, 'shared', {
      acquisition_status: 'failed',
      failure_reason: 'network',
    });

    const page = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    const lookup = client.getQueryData<ListTracksResponse>(libraryKeys.lookup('q'))!;
    const featuring = client.getQueryData<ListTracksResponse>(libraryKeys.featuring('identity'))!;
    const detail = client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;

    for (const copy of [
      page.pages[0]!.items[0],
      lookup.items[0],
      featuring.items[0],
      detail.tracks[0],
    ]) {
      expect(copy!.acquisition_status).toBe('failed');
      expect(copy!.failure_reason).toBe('network');
    }
    expect(page.pages[0]!.items[0]!.title).toBe('Page Copy');
    expect(lookup.items[0]!.title).toBe('Lookup Copy');
    expect(featuring.items[0]!.title).toBe('Featuring Copy');
    expect(detail.tracks[0]!.title).toBe('Detail Copy');
  });

  it('leaves every family untouched when the id is not present', () => {
    const client = newClient();
    const page = makePage([makeTrack({ id: asTrackId('a') })]);
    const lookup = makePage([makeTrack({ id: asTrackId('b') })]);
    seedTracksPrefix(client, [page]);
    client.setQueryData(libraryKeys.lookup('q'), lookup);

    patchTrackInCaches(client, 'not-cached', { acquisition_status: 'failed' });

    expect(
      client.getQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort')),
    ).toEqual(makeInfinite([page]));
    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookup);
  });

  it('is idempotent: applying the same patch twice equals applying it once', () => {
    const client = newClient();
    seedTracksPrefix(client, [
      makePage([makeTrack({ id: asTrackId('target'), title: 'Original' })]),
    ]);

    patchTrackInCaches(client, 'target', { title: 'Patched' });
    const afterFirst = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    patchTrackInCaches(client, 'target', { title: 'Patched' });
    const afterSecond = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    expect(afterSecond).toEqual(afterFirst);
  });

  it('leaves an unfetched (data-undefined) query untouched across every shape', () => {
    const client = newClient();
    registerUnfetchedQuery(client, libraryKeys.tracks('q', 'sort'));
    registerUnfetchedQuery(client, libraryKeys.lookup('q'));
    registerUnfetchedQuery(client, libraryKeys.featuring('identity'));
    registerUnfetchedQuery(client, playlistKeys.detail('p1'));

    expect(() => patchTrackInCaches(client, 'target', { acquisition_status: 'ready' })).not.toThrow();

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toBeUndefined();
    expect(client.getQueryData(libraryKeys.lookup('q'))).toBeUndefined();
    expect(client.getQueryData(libraryKeys.featuring('identity'))).toBeUndefined();
    expect(client.getQueryData(playlistKeys.detail('p1'))).toBeUndefined();
  });

  it('property: replaying an arbitrary patch never changes the outcome of the first application', () => {
    fc.assert(
      fc.property(
        fc.record({
          title: fc.string(),
          acquisition_status: fc.constantFrom('pending', 'ready', 'failed'),
        }),
        (patch) => {
          const client = newClient();
          seedTracksPrefix(client, [
            makePage([
              makeTrack({ id: asTrackId('target') }),
              makeTrack({ id: asTrackId('other') }),
            ]),
          ]);

          patchTrackInCaches(client, 'target', patch as Partial<TrackResponse>);
          const once = client.getQueryData<InfiniteData<ListTracksResponse>>(
            libraryKeys.tracks('q', 'sort'),
          );

          patchTrackInCaches(client, 'target', patch as Partial<TrackResponse>);
          const twice = client.getQueryData<InfiniteData<ListTracksResponse>>(
            libraryKeys.tracks('q', 'sort'),
          );

          expect(twice).toEqual(once);
        },
      ),
    );
  });
});
