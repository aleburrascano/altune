import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';
import { QueryClient, type InfiniteData } from '@tanstack/react-query';
import { asPlaylistId, asTrackId } from '@shared/apiClient/ids';
import type { PlaylistDetailResponse } from '@shared/apiClient/playlistResponse';
import type { ListTracksResponse, TrackResponse } from '@shared/apiClient/trackResponse';
import { libraryKeys, playlistKeys } from '@shared/query/queryKeys';
import {
  getTrackFromCaches,
  invalidateLibraryDerived,
  removeTrackFromCaches,
  replaceTrackInCaches,
  upsertTrackInCaches,
  captureTrackPlacements,
  restoreTrackPlacements,
  forgetTrack,
} from '@shared/events/trackCachePatch';
import { patchTrackInCaches } from '@shared/events/trackPatchBatch';
import {
  makeInfinite,
  makePage,
  makePlaylistDetail,
  makeTrack,
  newClient,
  registerUnfetchedQuery,
  seedTracksPrefix,
} from '@shared/events/__tests__/trackCacheFixtures';
import { toReady, toTrackStatus } from '@shared/apiClient/trackAcquisition';
import { patchTrackStatus, useTrackStatusStore } from '@shared/acquisition/state/trackStatusStore';

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

  it('does not carry a cached failure_message onto an incoming track that omits it', () => {
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

describe('paged offsets stay consistent with the rows the cache holds', () => {
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

describe('invalidateLibraryDerived', () => {
  it('invalidates every cache derived from library membership, once each', () => {
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

describe('track id branding', () => {
  it('refuses a bare string where a TrackId belongs', () => {
    expectType<Not<IsAssignable<string, Parameters<typeof removeTrackFromCaches>[1]>>>();
  });
});

describe('the loadAll array cache', () => {
  const track = {
    id: asTrackId('t1'),
    title: 'Track One',
    artist: 'Artist One',
    album: null,
    duration_seconds: 180,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
  } as unknown as TrackResponse;

  describe('the loadAll array cache', () => {
    it('lives outside the paged-tracks prefix', () => {
      expect(libraryKeys.tracksAll('', 'recent').slice(0, 2)).not.toEqual(libraryKeys.tracksPrefix);
    });

    it('does not crash the paged-cache patchers', () => {
      const qc = new QueryClient();
      qc.setQueryData(libraryKeys.tracksAll('', 'recent'), [track]);
      expect(() => removeTrackFromCaches(qc, track.id)).not.toThrow();
      expect(() => captureTrackPlacements(qc, track.id)).not.toThrow();
      expect(() => upsertTrackInCaches(qc, track)).not.toThrow();
      expect(() => getTrackFromCaches(qc, track.id)).not.toThrow();
    });
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

describe('forgetTrack', () => {
  it('clears the track from the query caches and the status store', () => {
    const client = new QueryClient();
    const id = asTrackId('target');
    client.setQueryData<ListTracksResponse>(libraryKeys.lookup('q'), {
      items: [{ id }],
      total: 1,
    } as unknown as ListTracksResponse);
    client.setQueryData<InfiniteData<ListTracksResponse>>(libraryKeys.tracks('q', 'sort'), {
      pages: [{ items: [{ id }], total: 1 } as unknown as ListTracksResponse],
      pageParams: [0],
    });
    patchTrackStatus(id, toTrackStatus(toReady()));

    forgetTrack(client, id);

    expect(client.getQueryData<ListTracksResponse>(libraryKeys.lookup('q'))!.items).toEqual([]);
    expect(useTrackStatusStore.getState().statuses[id]).toBeUndefined();
  });
});
