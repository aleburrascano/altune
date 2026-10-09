import {
  InfiniteQueryObserver,
  notifyManager,
  type InfiniteData,
  type QueryClient,
} from '@tanstack/react-query';
import { waitFor } from '@testing-library/react-native';
import fc from 'fast-check';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import { toFailed, toPending, toReady } from '@shared/api-client/trackAcquisition';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import type { ListTracksResponse } from '@shared/api-client/trackResponse';
import { libraryKeys, playlistKeys } from '@shared/query/queryKeys';

import { getTrackFromCaches } from '@shared/events/trackCachePatch';
import { patchTrackInCaches, scheduleTrackPatch } from '@shared/events/trackPatchBatch';
import { settleTrackPatches } from '@shared/events/__tests__/settleTrackPatches';
import {
  makeInfinite,
  makePage,
  makePlaylistDetail,
  makeTrack,
  newClient,
  registerUnfetchedQuery,
  seedTracksPrefix,
} from '@shared/events/__tests__/trackCacheFixtures';

describe('patchTrackInCaches', () => {
  const SEEDED_TOTAL = 9;

  it('applies a partial patch to the matching track in a page, preserving unrelated fields and total', () => {
    const client = newClient();
    const target = makeTrack({
      id: asTrackId('target'),
      acquisition_status: 'pending',
      title: 'Original',
    });
    const other = makeTrack({ id: asTrackId('other') });
    seedTracksPrefix(client, [makePage([target, other], { total: SEEDED_TOTAL })]);

    patchTrackInCaches(client, asTrackId('target'), toReady());

    const result = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    )!;
    expect(result.pages[0]!.items[0]).toEqual({
      ...target,
      ...toReady(),
    });
    expect(result.pages[0]!.items[1]).toEqual(other);
    expect(result.pages[0]!.total).toBe(SEEDED_TOTAL);
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
  function cachedStatus(client: QueryClient): string {
    const data = client.getQueryData<InfiniteData<ListTracksResponse>>(
      libraryKeys.tracks('q', 'sort'),
    );
    return data!.pages[0]!.items[0]!.acquisition_status;
  }

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

describe('a late REST response cannot regress a patch that landed mid-fetch', () => {
  const key = libraryKeys.tracks('q', 'sort');
  type Transition = ReturnType<typeof toReady | typeof toPending | typeof toFailed>;
  function trackX(transition: Transition) {
    return makePage([makeTrack({ id: asTrackId('x'), ...transition })]);
  }

  function slowStaleServer() {
    let release: (() => void) | undefined;
    const queryFn = jest.fn(() => {
      if (queryFn.mock.calls.length > 1) return Promise.resolve(trackX(toReady()));
      return new Promise<ListTracksResponse>((resolve) => {
        release = () => resolve(trackX(toPending()));
      });
    });
    return { queryFn, release: () => release?.() };
  }

  function mountLibrary(queryFn: () => Promise<ListTracksResponse>, rendered: string[] = []) {
    const observer = new InfiniteQueryObserver(client, {
      queryKey: key,
      queryFn,
      initialPageParam: 0,
      getNextPageParam: () => undefined,
      retry: false,
    });
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
  afterEach(() => client.clear());

  async function raceSseAgainstSlowFetch(): Promise<string[]> {
    const server = slowStaleServer();
    const rendered: string[] = [];
    const unmount = mountLibrary(server.queryFn, rendered);
    await waitFor(() => expect(client.getQueryState(key)?.fetchStatus).toBe('fetching'));
    rendered.length = 0;

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
