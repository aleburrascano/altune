import {
  InfiniteQueryObserver,
  notifyManager,
  QueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { waitFor } from '@testing-library/react-native';
import fc from 'fast-check';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import { toFailed, toPending, toReady } from '@shared/api-client/trackAcquisition';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import {
  captureTrackPlacements,
  getTrackFromCaches,
  invalidateLibraryDerived,
  patchTrackInCaches,
  removeTrackFromCaches,
  replaceTrackInCaches,
  restoreTrackPlacements,
  scheduleTrackPatch,
  upsertTrackInCaches,
} from '../trackCachePatch';
import { settleTrackPatches } from './settleTrackPatches';

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
    expect(getTrackFromCaches(client, asTrackId('t1'))).toBeUndefined();
  });

  it('finds a track on a later page of a paged tracksPrefix cache', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('a') })]), makePage([target])]);

    expect(getTrackFromCaches(client, asTrackId('target'))).toEqual(target);
  });

  it('prefers the tracksPrefix copy over a stale copy in the lookup cache', () => {
    const client = newClient();
    const fresh = makeTrack({ id: asTrackId('shared'), title: 'Fresh Title' });
    const stale = makeTrack({ id: asTrackId('shared'), title: 'Stale Title' });
    seedTracksPrefix(client, [makePage([fresh])]);
    client.setQueryData(libraryKeys.lookup('q'), makePage([stale]));

    expect(getTrackFromCaches(client, asTrackId('shared'))?.title).toBe('Fresh Title');
  });

  it('falls back to the lookup cache when the track is not in any paged cache', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('lookup-only') });
    client.setQueryData(libraryKeys.lookup('q'), makePage([target]));

    expect(getTrackFromCaches(client, asTrackId('lookup-only'))).toEqual(target);
  });

  it('falls back to the featuring cache', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('featuring-only') });
    client.setQueryData(libraryKeys.featuring('identity'), makePage([target]));

    expect(getTrackFromCaches(client, asTrackId('featuring-only'))).toEqual(target);
  });

  it('falls back to a cached playlist detail when not found elsewhere', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('playlist-only') });
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [target]),
    );

    expect(getTrackFromCaches(client, asTrackId('playlist-only'))).toEqual(target);
  });

  it('skips a family whose query is registered but not yet fetched (data undefined)', () => {
    const client = newClient();
    registerUnfetchedQuery(client, libraryKeys.tracks('q', 'sort'));
    registerUnfetchedQuery(client, libraryKeys.lookup('q'));
    registerUnfetchedQuery(client, libraryKeys.featuring('identity'));
    registerUnfetchedQuery(client, playlistKeys.detail(asPlaylistId('p1')));

    expect(getTrackFromCaches(client, asTrackId('anything'))).toBeUndefined();
  });

  it('returns undefined for an id absent from every populated family', () => {
    const client = newClient();
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('a') })])]);
    client.setQueryData(libraryKeys.lookup('q'), makePage([makeTrack({ id: asTrackId('b') })]));
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [makeTrack({ id: asTrackId('c') })]),
    );

    expect(getTrackFromCaches(client, asTrackId('missing'))).toBeUndefined();
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
    // The prepended row renumbers the list: page 2 now starts one position later (#792).
    expect(result.pages[1]).toEqual({ ...page2, offset: page2.offset + 1 });
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

  it('does not carry a cached failure_message onto an incoming track that omits it (#933)', () => {
    const client = newClient();
    seedTracksPrefix(client, [
      makePage([
        makeTrack({
          id: asTrackId('b'),
          acquisition_status: 'failed',
          failure_reason: 'no_source',
          failure_message: 'No source found',
          audio_ref: 'client-ref',
        }),
      ]),
    ]);

    upsertTrackInCaches(client, makeTrack({ id: asTrackId('b'), acquisition_status: 'ready' }));

    const merged = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!.pages[0]!.items[0]!;
    expect(merged.acquisition_status).toBe('ready');
    expect(merged.failure_reason).toBeNull();
    expect(merged).not.toHaveProperty('failure_message');
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
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), detailBefore);

    upsertTrackInCaches(client, makeTrack({ id: asTrackId('shared'), title: 'New' }));

    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookupBefore);
    expect(client.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(detailBefore);
  });
});

describe('replaceTrackInCaches', () => {
  it('swaps an optimistic id for the real track, preserving its position and total', () => {
    const client = newClient();
    const optimistic = makeTrack({ id: asTrackId('optimistic-1') });
    const other = makeTrack({ id: asTrackId('other') });
    seedTracksPrefix(client, [makePage([optimistic, other])]);

    const real = makeTrack({ id: asTrackId('real-1'), title: 'Server Title' });
    replaceTrackInCaches(client, asTrackId('optimistic-1'), real);

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

    replaceTrackInCaches(client, asTrackId('optimistic-1'), real);

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

    replaceTrackInCaches(
      client,
      asTrackId('no-such-optimistic-id'),
      makeTrack({ id: asTrackId('real-1') }),
    );

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

    replaceTrackInCaches(client, asTrackId('optimistic-1'), real);
    const afterFirst = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    replaceTrackInCaches(client, asTrackId('optimistic-1'), real);
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

    removeTrackFromCaches(client, asTrackId('target'));

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

    removeTrackFromCaches(client, asTrackId('target'));

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
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [target, makeTrack({ id: asTrackId('a') })]),
    );
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p2')),
      makePlaylistDetail('p2', [makeTrack({ id: asTrackId('b') }), target]),
    );

    removeTrackFromCaches(client, asTrackId('target'));

    const p1 = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    const p2 = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p2')),
    )!;
    expect(p1.tracks.map((t) => t.id)).toEqual(['a']);
    expect(p2.tracks.map((t) => t.id)).toEqual(['b']);
  });

  it('keeps a playlist detail track_count consistent with its tracks after the removal', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [target, makeTrack({ id: asTrackId('a') })], { track_count: 2 }),
    );

    removeTrackFromCaches(client, asTrackId('target'));

    const p1 = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(p1.track_count).toBe(p1.tracks.length);
  });

  it('is idempotent: removing an already-removed track a second time makes no further change', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    seedTracksPrefix(client, [makePage([target, makeTrack({ id: asTrackId('a') })], { total: 6 })]);

    removeTrackFromCaches(client, asTrackId('target'));
    const afterFirst = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    removeTrackFromCaches(client, asTrackId('target'));
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
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), detail);

    removeTrackFromCaches(client, asTrackId('not-cached'));

    expect(
      client.getQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort')),
    ).toEqual(makeInfinite([page]));
    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookup);
    expect(client.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(detail);
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

    patchTrackInCaches(client, asTrackId('target'), toReady());

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items[0]).toEqual({
      ...target,
      ...toReady(),
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
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [inDetail]),
    );

    patchTrackInCaches(client, asTrackId('shared'), toFailed('network', null));

    const page = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    const lookup = client.getQueryData<ListTracksResponse>(libraryKeys.lookup('q'))!;
    const featuring = client.getQueryData<ListTracksResponse>(libraryKeys.featuring('identity'))!;
    const detail = client.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;

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

    patchTrackInCaches(client, asTrackId('not-cached'), toFailed(null, null));

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

    patchTrackInCaches(client, asTrackId('target'), { title: 'Patched' });
    const afterFirst = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );

    patchTrackInCaches(client, asTrackId('target'), { title: 'Patched' });
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
    registerUnfetchedQuery(client, playlistKeys.detail(asPlaylistId('p1')));

    expect(() => patchTrackInCaches(client, asTrackId('target'), toReady())).not.toThrow();

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toBeUndefined();
    expect(client.getQueryData(libraryKeys.lookup('q'))).toBeUndefined();
    expect(client.getQueryData(libraryKeys.featuring('identity'))).toBeUndefined();
    expect(client.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toBeUndefined();
  });

  it('property: replaying an arbitrary patch never changes the outcome of the first application', () => {
    fc.assert(
      fc.property(
        fc.record({
          title: fc.string(),
          acquisition: fc.constantFrom(toPending(), toReady(), toFailed('network', null)),
        }),
        ({ title, acquisition }) => {
          const patch = { title, ...acquisition };
          const client = newClient();
          seedTracksPrefix(client, [
            makePage([
              makeTrack({ id: asTrackId('target') }),
              makeTrack({ id: asTrackId('other') }),
            ]),
          ]);

          patchTrackInCaches(client, asTrackId('target'), patch);
          const once = client.getQueryData<InfiniteData<ListTracksResponse>>(
            libraryKeys.tracks('q', 'sort'),
          );

          patchTrackInCaches(client, asTrackId('target'), patch);
          const twice = client.getQueryData<InfiniteData<ListTracksResponse>>(
            libraryKeys.tracks('q', 'sort'),
          );

          expect(twice).toEqual(once);
        },
      ),
    );
  });
});

describe('scheduleTrackPatch', () => {
  const cachedStatus = (client: QueryClient): string =>
    client.getQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort'))!
      .pages[0]!.items[0]!.acquisition_status;

  function seedOnePendingTrack(client: QueryClient): void {
    seedTracksPrefix(client, [
      makePage([makeTrack({ id: asTrackId('target'), acquisition_status: 'pending' })]),
    ]);
  }

  it('lands in the cache of the client it was scheduled against, not the one scheduled next', async () => {
    const first = newClient();
    const second = newClient();
    seedOnePendingTrack(first);
    seedOnePendingTrack(second);

    scheduleTrackPatch(first, asTrackId('target'), toReady());
    scheduleTrackPatch(second, asTrackId('target'), toFailed('network', null));
    await settleTrackPatches();

    expect(cachedStatus(first)).toBe('ready');
    expect(cachedStatus(second)).toBe('failed');
  });

  it('shows a reader the patch it has scheduled before the batched pass applies it', () => {
    const client = newClient();
    seedOnePendingTrack(client);

    scheduleTrackPatch(client, asTrackId('target'), toFailed('network', 'the radio went out'));

    expect(cachedStatus(client)).toBe('pending');
    expect(getTrackFromCaches(client, asTrackId('target'))?.acquisition_status).toBe('failed');
    expect(getTrackFromCaches(client, asTrackId('target'))?.failure_message).toBe(
      'the radio went out',
    );
  });
});

describe('paged offsets stay consistent with the rows the cache holds (#792)', () => {
  const ids = (prefix: string, n: number) =>
    Array.from({ length: n }, (_, i) => makeTrack({ id: asTrackId(`${prefix}${i}`) }));
  const threePages = () => [
    makePage(ids('a', 3), { offset: 0, limit: 3, total: 9, has_more: true }),
    makePage(ids('b', 3), { offset: 3, limit: 3, total: 9, has_more: true }),
    makePage(ids('c', 3), { offset: 6, limit: 3, total: 9, has_more: true }),
  ];
  const offsets = (client: QueryClient) =>
    client
      .getQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort'))!
      .pages.map((p) => p.offset);

  it('shifts every later page back when a track is removed from an earlier page', () => {
    const client = newClient();
    seedTracksPrefix(client, threePages());

    removeTrackFromCaches(client, asTrackId('a1'));

    expect(offsets(client)).toEqual([0, 2, 5]);
  });

  it('shifts only the pages after the one the track was removed from', () => {
    const client = newClient();
    seedTracksPrefix(client, threePages());

    removeTrackFromCaches(client, asTrackId('b0'));

    expect(offsets(client)).toEqual([0, 3, 5]);
  });

  it('shifts later pages back when a replacement drops a duplicate row', () => {
    const client = newClient();
    const pages = threePages();
    pages[0] = { ...pages[0]!, items: [...ids('a', 2), makeTrack({ id: asTrackId('real') })] };
    seedTracksPrefix(client, pages);

    replaceTrackInCaches(client, asTrackId('a0'), makeTrack({ id: asTrackId('real') }));

    expect(offsets(client)).toEqual([0, 2, 5]);
  });

  it('never drives an offset negative when the cached offsets are already inconsistent', () => {
    const client = newClient();
    const pages = threePages().map((page) => ({ ...page, offset: 0 }));
    seedTracksPrefix(client, pages);

    removeTrackFromCaches(client, asTrackId('a1'));

    expect(offsets(client)).toEqual([0, 0, 0]);
  });

  it('leaves offsets alone for a patch that changes no row count', () => {
    const client = newClient();
    seedTracksPrefix(client, threePages());

    patchTrackInCaches(client, asTrackId('a1'), { title: 'Renamed' });

    expect(offsets(client)).toEqual([0, 3, 6]);
  });

  it('round-trips offsets through a removal and its rollback', () => {
    const client = newClient();
    const pages = threePages();
    seedTracksPrefix(client, pages);

    const placements = captureTrackPlacements(client, asTrackId('a1'));
    removeTrackFromCaches(client, asTrackId('a1'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toEqual(makeInfinite(pages));
  });

  it('round-trips offsets through an optimistic insert that is then removed', () => {
    const client = newClient();
    seedTracksPrefix(client, threePages());

    upsertTrackInCaches(client, makeTrack({ id: asTrackId('optimistic') }));
    expect(offsets(client)).toEqual([0, 4, 7]);
    removeTrackFromCaches(client, asTrackId('optimistic'));

    expect(offsets(client)).toEqual([0, 3, 6]);
  });
});

describe('captureTrackPlacements + restoreTrackPlacements — undo an optimistic removal', () => {
  it('round-trips a removal across every family back to the exact prior data', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    const pages = [
      makePage([makeTrack({ id: asTrackId('a') })], { total: 5 }),
      makePage([makeTrack({ id: asTrackId('b') }), target], { total: 8 }),
    ];
    seedTracksPrefix(client, pages);
    const lookup = makePage([target, makeTrack({ id: asTrackId('c') })], { total: 9 });
    client.setQueryData(libraryKeys.lookup('q'), lookup);
    const detail = makePlaylistDetail('p1', [target, makeTrack({ id: asTrackId('d') }), target]);
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), detail);

    const placements = captureTrackPlacements(client, asTrackId('target'));
    removeTrackFromCaches(client, asTrackId('target'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toEqual(makeInfinite(pages));
    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookup);
    expect(client.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(detail);
  });

  it('keeps a removal made by another mutation in the meantime', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    const other = makeTrack({ id: asTrackId('other') });
    client.setQueryData(libraryKeys.featuring('who'), makePage([other, target], { total: 2 }));

    const placements = captureTrackPlacements(client, asTrackId('target'));
    removeTrackFromCaches(client, asTrackId('target'));
    removeTrackFromCaches(client, asTrackId('other'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.featuring('who'))).toEqual(
      makePage([target], { total: 1 }),
    );
  });

  it('leaves an entry alone when the track is already back, so it never duplicates', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    seedTracksPrefix(client, [makePage([target], { total: 1 })]);
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [target]),
    );

    const placements = captureTrackPlacements(client, asTrackId('target'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toEqual(
      makeInfinite([makePage([target], { total: 1 })]),
    );
    expect(
      client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!.tracks,
    ).toEqual([target]);
  });

  it('captures nothing for an absent track and skips entries evicted before restore', () => {
    const client = newClient();
    expect(captureTrackPlacements(client, asTrackId('missing'))).toEqual([]);

    const target = makeTrack({ id: asTrackId('target') });
    client.setQueryData(libraryKeys.lookup('q'), makePage([target]));
    registerUnfetchedQuery(client, libraryKeys.featuring('none'));
    const placements = captureTrackPlacements(client, asTrackId('target'));
    client.removeQueries({ queryKey: libraryKeys.lookup('q') });
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.lookup('q'))).toBeUndefined();
  });
});

describe('invalidateLibraryDerived', () => {
  it('invalidates every cache derived from library membership, once each (#938)', () => {
    const queryClient = new QueryClient();
    const spy = jest.spyOn(queryClient, 'invalidateQueries');

    invalidateLibraryDerived(queryClient);

    expect(spy.mock.calls.map(([filters]) => filters?.queryKey)).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.lookupPrefix,
    ]);
  });
});

describe('a late REST response cannot regress a patch that landed mid-fetch (#961)', () => {
  const key = libraryKeys.tracks('q', 'sort');
  const trackX = (transition: ReturnType<typeof toReady | typeof toPending | typeof toFailed>) =>
    makePage([makeTrack({ id: asTrackId('x'), ...transition })]);

  // GET /tracks: the first request is held open until release() and then answers
  // with the server's view from when it was sent (X pending); any later request
  // sees the current view (X ready), as the server has finished acquiring X.
  function slowStaleServer() {
    let release: () => void = () => undefined;
    const queryFn = jest.fn(() => {
      if (queryFn.mock.calls.length > 1) return Promise.resolve(trackX(toReady()));
      return new Promise<ListTracksResponse>((resolve) => {
        release = () => resolve(trackX(toPending()));
      });
    });
    return { queryFn, release: () => release() };
  }

  // Mounts the library's infinite query the way useLibraryTracks does; every status of
  // X the screen would render is pushed onto `rendered`. Returns unmount.
  function mountLibrary(queryFn: () => Promise<ListTracksResponse>, rendered: string[] = []) {
    const observer = new InfiniteQueryObserver(client, {
      queryKey: key,
      queryFn,
      initialPageParam: 0,
      getNextPageParam: () => undefined,
      retry: false,
    });
    // Subscribe and read exactly as useBaseQuery does: a batched store-change callback,
    // then useSyncExternalStore's snapshot, observer.getCurrentResult().
    return observer.subscribe(
      notifyManager.batchCalls(() => {
        const { data } = observer.getCurrentResult();
        const x = data?.pages.flatMap((p) => p.items).find((t) => t.id === 'x');
        if (x) rendered.push(x.acquisition_status);
      }),
    );
  }

  function statusOfX() {
    return getTrackFromCaches(client, asTrackId('x'))?.acquisition_status;
  }

  let client: QueryClient;
  beforeEach(() => {
    client = newClient();
  });
  // Drops the queries, and with them their gc timers, so jest can exit.
  afterEach(() => client.clear());

  // Returns every status of X the screen rendered from the SSE patch onwards.
  async function raceSseAgainstSlowFetch(): Promise<string[]> {
    const server = slowStaleServer();
    const rendered: string[] = [];
    const unmount = mountLibrary(server.queryFn, rendered);
    await waitFor(() => expect(client.getQueryState(key)?.fetchStatus).toBe('fetching'));
    rendered.length = 0;

    // SSE track_acquisition_completed lands while GET /tracks is still in flight.
    patchTrackInCaches(client, asTrackId('x'), toReady());
    server.release();

    await waitFor(() => expect(client.getQueryState(key)?.fetchStatus).toBe('idle'));
    unmount();
    return rendered;
  }

  it('keeps an SSE ready patch when a refetch that started earlier resolves pending', async () => {
    client.setQueryData(key, makeInfinite([trackX(toPending())]));

    await raceSseAgainstSlowFetch();

    expect(statusOfX()).toBe('ready');
  });

  it('ends ready when the SSE event lands during the first load of the list', async () => {
    await raceSseAgainstSlowFetch();

    expect(statusOfX()).toBe('ready');
  });

  it('never lets the screen render the regressed pending row, even for one frame', async () => {
    client.setQueryData(key, makeInfinite([trackX(toPending())]));

    const rendered = await raceSseAgainstSlowFetch();

    expect(rendered).not.toContain('pending');
  });

  it('lets a fetch that starts after the patch deliver newer server state', async () => {
    client.setQueryData(key, makeInfinite([trackX(toPending())]));
    patchTrackInCaches(client, asTrackId('x'), toReady());

    const unmount = mountLibrary(() => Promise.resolve(trackX(toFailed('no_match', null))));
    await waitFor(() => expect(client.getQueryState(key)?.status).toBe('success'));
    await waitFor(() => expect(client.getQueryState(key)?.fetchStatus).toBe('idle'));
    unmount();

    expect(statusOfX()).toBe('failed');
  });

  it('stops replaying once the raced fetch has settled', async () => {
    await raceSseAgainstSlowFetch();

    const unmount = mountLibrary(() => Promise.resolve(trackX(toPending())));
    await client.refetchQueries({ queryKey: key, exact: true });
    unmount();

    expect(statusOfX()).toBe('pending');
  });
});

describe('track id branding', () => {
  // Compile-time guard: tsc fails if these cache writers start accepting a bare string again,
  // which is what let an id nobody had parsed select which rows a patch rewrote.
  it('refuses a bare string where a TrackId belongs', () => {
    const client = newClient();
    seedTracksPrefix(client, [makePage([makeTrack({ id: asTrackId('target') })], { total: 1 })]);

    // @ts-expect-error a raw string must go through asTrackId / parseTrackId first
    removeTrackFromCaches(client, 'target');

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items).toEqual([]);
  });
});
