import type { InfiniteData, QueryClient, QueryFilters, QueryKey } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionTransition } from '@shared/api-client/trackAcquisition';
import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackFields,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

type TrackPages = InfiniteData<ListTracksResponse>;

// The single declaration of every query cache family that stores a Track. Read,
// remove and patch iterate this list (in order); RESYNC_KEYS references it by name.
// Adding a new Track-bearing cache is one entry here, not an edit at five sites.
type TrackCacheShape = 'paged' | 'flat' | 'playlistDetails';

interface TrackCacheFamily {
  readonly prefix: readonly string[];
  readonly shape: TrackCacheShape;
}

export const TRACK_CACHE_FAMILIES = {
  pagedLibrary: { prefix: libraryKeys.tracksPrefix, shape: 'paged' },
  lookup: { prefix: libraryKeys.lookupPrefix, shape: 'flat' },
  featuring: { prefix: libraryKeys.featuringPrefix, shape: 'flat' },
  playlistDetails: { prefix: playlistKeys.details, shape: 'playlistDetails' },
} as const satisfies Record<string, TrackCacheFamily>;

const TRACK_CACHE_FAMILY_LIST: readonly TrackCacheFamily[] = Object.values(TRACK_CACHE_FAMILIES);

// What goes stale when a track joins or leaves the library: the aggregates built
// from membership (album and artist groupings, the library summary) and the lookup
// cache. The one policy every add/delete site uses, so they cannot drift apart.
const LIBRARY_DERIVED_KEYS: readonly (readonly string[])[] = [
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  libraryKeys.lookupPrefix,
];

/** Marks every cache derived from library membership stale after a track is added or removed. */
export function invalidateLibraryDerived(queryClient: QueryClient): void {
  for (const queryKey of LIBRARY_DERIVED_KEYS) {
    void queryClient.invalidateQueries({ queryKey });
  }
}

type MapItems = (items: TrackResponse[]) => TrackResponse[];
type AdjustTotal = (total: number, before: number, after: number) => number;

interface WritePolicy {
  readonly listTotal: AdjustTotal;
  readonly playlistCount: AdjustTotal;
}

const keepTotal: AdjustTotal = (total) => total;

const REMOVE_POLICY: WritePolicy = {
  listTotal: (total, before, after) => total - (before - after),
  playlistCount: (_count, _before, after) => after,
};

const PATCH_POLICY: WritePolicy = {
  listTotal: keepTotal,
  playlistCount: keepTotal,
};

/**
 * Keep each page's `offset` equal to where its first row now sits in the list the
 * cache implies. fetchNextPage derives the next offset from the last page's
 * `offset + items.length`, so a row removed from (or added to) an earlier page must
 * shift every later page, or the next fetch skips (or repeats) a track.
 */
function reflowOffsets(
  before: readonly ListTracksResponse[],
  after: readonly ListTracksResponse[],
): ListTracksResponse[] {
  let shift = 0;
  return after.map((page, i) => {
    const shifted = shift === 0 ? page : { ...page, offset: Math.max(0, page.offset + shift) };
    shift += page.items.length - (before[i]?.items.length ?? page.items.length);
    return shifted;
  });
}

function mapPages(
  prev: TrackPages | undefined,
  mapItems: MapItems,
  adjustTotal: AdjustTotal,
): TrackPages | undefined {
  if (!prev) return prev;
  const pages = prev.pages.map((page) => {
    const items = mapItems(page.items);
    return { ...page, items, total: adjustTotal(page.total, page.items.length, items.length) };
  });
  return { ...prev, pages: reflowOffsets(prev.pages, pages) };
}

function familyItems(shape: TrackCacheShape, data: unknown): readonly TrackResponse[] {
  if (!data) return [];
  switch (shape) {
    case 'paged':
      return (data as TrackPages).pages.flatMap((page) => page.items);
    case 'flat':
      return (data as ListTracksResponse).items;
    case 'playlistDetails':
      return (data as PlaylistDetailResponse).tracks;
  }
}

// Writes every query `filters` matches: the family prefix for all of them, or one
// query's exact key to touch just that entry.
function writeFamily(
  queryClient: QueryClient,
  family: TrackCacheFamily,
  filters: QueryFilters,
  mapItems: MapItems,
  policy: WritePolicy,
): void {
  switch (family.shape) {
    case 'paged':
      queryClient.setQueriesData<TrackPages>(filters, (prev) =>
        mapPages(prev, mapItems, policy.listTotal),
      );
      return;
    case 'flat':
      queryClient.setQueriesData<ListTracksResponse>(filters, (prev) => {
        if (!prev) return prev;
        const items = mapItems(prev.items);
        return {
          ...prev,
          items,
          total: policy.listTotal(prev.total, prev.items.length, items.length),
        };
      });
      return;
    case 'playlistDetails':
      queryClient.setQueriesData<PlaylistDetailResponse>(filters, (prev) => {
        if (!prev) return prev;
        const tracks = mapItems(prev.tracks);
        return {
          ...prev,
          tracks,
          track_count: policy.playlistCount(prev.track_count, prev.tracks.length, tracks.length),
        };
      });
      return;
  }
}

export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackResponse | undefined {
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    const entries = queryClient.getQueriesData({ queryKey: family.prefix });
    for (const [, data] of entries) {
      const found = familyItems(family.shape, data).find((t) => t.id === trackId);
      if (found) return withPatch(found, pendingPatchFor(queryClient, trackId));
    }
  }
  return undefined;
}

// next is a whole track, so its acquisition triple wins outright: prev only
// contributes fields next omits (e.g. a client-only audio_ref), never a
// failure_message next didn't send.
function mergeTrack(prev: TrackResponse, next: TrackResponse): TrackResponse {
  const merged = { ...prev, ...next };
  if (next.failure_message === undefined) delete merged.failure_message;
  // Sound once the stale message is gone: status and reason both come from next.
  return merged as TrackResponse;
}

export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  flushTrackCachePatches();
  queryClient.setQueriesData<TrackPages>(
    { queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix },
    (prev) => {
      if (!prev) return prev;
      const known = prev.pages.some((page) => page.items.some((t) => t.id === track.id));
      if (known) {
        return mapPages(
          prev,
          (items) => items.map((t) => (t.id === track.id ? mergeTrack(t, track) : t)),
          keepTotal,
        );
      }
      const [first, ...rest] = prev.pages;
      if (!first) return prev;
      const pages = [{ ...first, items: [track, ...first.items], total: first.total + 1 }, ...rest];
      return { ...prev, pages: reflowOffsets(prev.pages, pages) };
    },
  );
}

export function replaceTrackInCaches(
  queryClient: QueryClient,
  optimisticId: TrackId,
  real: TrackResponse,
): void {
  flushTrackCachePatches();
  queryClient.setQueriesData<TrackPages>(
    { queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix },
    (prev) =>
      mapPages(
        prev,
        (items) => dedupById(items.map((t) => (t.id === optimisticId ? real : t))),
        (total, before, after) => total - (before - after),
      ),
  );
}

function dedupById(items: TrackResponse[]): TrackResponse[] {
  const seen = new Set<string>();
  return items.filter((t) => (seen.has(t.id) ? false : (seen.add(t.id), true)));
}

export function removeTrackFromCaches(queryClient: QueryClient, trackId: TrackId): void {
  flushTrackCachePatches();
  const drop: MapItems = (items) => items.filter((t) => t.id !== trackId);
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(queryClient, family, { queryKey: family.prefix }, drop, REMOVE_POLICY);
  }
}

/**
 * Where a track sat in one cache entry: the query, the page (0 for unpaged shapes)
 * and every index it occupied. Captured before an optimistic removal so a failed
 * mutation can put the track back exactly where it was.
 */
export interface TrackCachePlacement {
  readonly queryKey: QueryKey;
  readonly shape: TrackCacheShape;
  readonly pageIndex: number;
  readonly indices: readonly number[];
  readonly track: TrackResponse;
}

function indicesOf(items: readonly TrackResponse[], trackId: TrackId): number[] {
  const out: number[] = [];
  items.forEach((t, i) => {
    if (t.id === trackId) out.push(i);
  });
  return out;
}

function unpagedItems(shape: TrackCacheShape, data: unknown): readonly TrackResponse[] {
  return shape === 'flat'
    ? (data as ListTracksResponse).items
    : (data as PlaylistDetailResponse).tracks;
}

export function captureTrackPlacements(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackCachePlacement[] {
  flushTrackCachePatches();
  const placements: TrackCachePlacement[] = [];
  const record = (
    queryKey: QueryKey,
    shape: TrackCacheShape,
    pageIndex: number,
    items: readonly TrackResponse[],
  ) => {
    const indices = indicesOf(items, trackId);
    const track = items[indices[0] ?? -1];
    if (track) placements.push({ queryKey, shape, pageIndex, indices, track });
  };
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    for (const [queryKey, data] of queryClient.getQueriesData({ queryKey: family.prefix })) {
      if (!data) continue;
      if (family.shape === 'paged') {
        (data as TrackPages).pages.forEach((page, i) => record(queryKey, 'paged', i, page.items));
      } else {
        record(queryKey, family.shape, 0, unpagedItems(family.shape, data));
      }
    }
  }
  return placements;
}

function reinsert(items: TrackResponse[], placement: TrackCachePlacement): TrackResponse[] {
  const next = [...items];
  for (const index of placement.indices) {
    next.splice(Math.min(index, next.length), 0, placement.track);
  }
  return next;
}

/**
 * Undo an optimistic removal: put the track back at each captured position,
 * restoring the totals the removal decremented. An entry that already holds the
 * track again (a refetch or server event landed first) is left alone, so the
 * rollback never duplicates a row or clobbers fresher state.
 */
export function restoreTrackPlacements(
  queryClient: QueryClient,
  placements: readonly TrackCachePlacement[],
): void {
  flushTrackCachePatches();
  for (const placement of placements) {
    const id = placement.track.id;
    const added = placement.indices.length;
    switch (placement.shape) {
      case 'paged':
        queryClient.setQueryData<TrackPages>(placement.queryKey, (prev) => {
          if (!prev?.pages[placement.pageIndex]) return prev;
          if (prev.pages.some((page) => page.items.some((t) => t.id === id))) return prev;
          const pages = prev.pages.map((page, i) =>
            i === placement.pageIndex
              ? { ...page, items: reinsert(page.items, placement), total: page.total + added }
              : page,
          );
          return { ...prev, pages: reflowOffsets(prev.pages, pages) };
        });
        break;
      case 'flat':
        queryClient.setQueryData<ListTracksResponse>(placement.queryKey, (prev) => {
          if (!prev || prev.items.some((t) => t.id === id)) return prev;
          return { ...prev, items: reinsert(prev.items, placement), total: prev.total + added };
        });
        break;
      case 'playlistDetails':
        queryClient.setQueryData<PlaylistDetailResponse>(placement.queryKey, (prev) => {
          if (!prev || prev.tracks.some((t) => t.id === id)) return prev;
          const tracks = reinsert(prev.tracks, placement);
          return { ...prev, tracks, track_count: tracks.length };
        });
        break;
    }
  }
}

// A cache patch either leaves the acquisition triple untouched or replaces it
// whole with a toPending/toReady/toFailed transition; a bare
// `{ acquisition_status }` that would strand stale failure text doesn't compile.
export type TrackPatch = Partial<TrackFields> &
  (
    | AcquisitionTransition
    | { acquisition_status?: never; failure_reason?: never; failure_message?: never }
  );

function withPatch(track: TrackResponse, patch: TrackPatch | undefined): TrackResponse {
  return patch ? { ...track, ...patch } : track;
}

// The patches waiting for the end of the tick, merged per track and tied to the client
// they were made against, so a client that never sees its flush cannot carry them into
// another one's cache.
interface PendingTrackPatches {
  readonly queryClient: QueryClient;
  readonly byTrackId: Map<TrackId, TrackPatch>;
}

let pendingPatches: PendingTrackPatches | null = null;

/**
 * Merges `patch` into the batch that lands at the end of this tick, so a burst of
 * acquisition events costs one pass over each cached family rather than one per event
 * (#1796). Spreading the merged patches equals applying each in turn, so the settled
 * cache is the one the unbatched sequence produced.
 *
 * A promise microtask rather than queueMicrotask, because a fake clock replaces the
 * latter, which would strand a scheduled patch until something advanced the timers.
 */
export function scheduleTrackPatch(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  if (pendingPatches && pendingPatches.queryClient !== queryClient) flushTrackCachePatches();
  if (pendingPatches === null) {
    pendingPatches = { queryClient, byTrackId: new Map() };
    void Promise.resolve().then(flushTrackCachePatches);
  }
  const prior = pendingPatches.byTrackId.get(trackId);
  pendingPatches.byTrackId.set(trackId, prior ? { ...prior, ...patch } : patch);
}

function pendingPatchFor(queryClient: QueryClient, trackId: TrackId): TrackPatch | undefined {
  return pendingPatches?.queryClient === queryClient
    ? pendingPatches.byTrackId.get(trackId)
    : undefined;
}

/**
 * Applies the scheduled patches now. Every other writer here flushes first, so a patch
 * keeps its place in the sequence against the add, remove or replace it arrived between,
 * and a reader sees a scheduled patch through `withPatch` without forcing a pass.
 */
function flushTrackCachePatches(): void {
  const batch = pendingPatches;
  if (batch === null) return;
  pendingPatches = null;
  const applyBatch: MapItems = (items) => items.map((t) => withPatch(t, batch.byTrackId.get(t.id)));
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(batch.queryClient, family, { queryKey: family.prefix }, applyBatch, PATCH_POLICY);
    replayOverInFlightFetches(batch.queryClient, family, applyBatch);
  }
}

/** Applies the patch to every cached copy of the track before returning. */
export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  scheduleTrackPatch(queryClient, trackId, patch);
  flushTrackCachePatches();
}

/**
 * A patch is newer than any fetch already in flight for its family: that response
 * describes the server as of when the request was sent, and TanStack writes it over
 * the cache unconditionally on arrival, so it would silently undo the patch (#961).
 * Re-apply the patch to exactly that response, in the same synchronous notify pass
 * that stored it, so observers never render the regressed row. Listening ends when
 * the fetch settles either way; an error or a cancel leaves the patched data in
 * place. Several patches during one fetch replay in the order they were made.
 *
 * Why not cancel and refetch instead: a burst of acquisition events (a playlist
 * import) would cancel every fetch that started, so a first load never finished.
 * A refetch that silently supersedes the raced one (cancelRefetch) keeps the
 * subscription and replays onto its response too; were the patch older than that
 * response, the server change in between emits its own event and patches again.
 */
function replayOverInFlightFetches(
  queryClient: QueryClient,
  family: TrackCacheFamily,
  mapItems: MapItems,
): void {
  const cache = queryClient.getQueryCache();
  const inFlight = cache
    .findAll({ queryKey: family.prefix })
    .filter((query) => query.state.fetchStatus !== 'idle');
  for (const query of inFlight) {
    const unsubscribe = cache.subscribe((event) => {
      if (event.query !== query) return;
      if (event.type === 'removed') return unsubscribe();
      // Observer events for this query fire before its own 'updated' event, already
      // seeing the settled state, so only 'updated' may end the subscription.
      if (event.type !== 'updated') return;
      if (event.action.type === 'success' && !event.action.manual) {
        const exactQuery = { queryKey: query.queryKey, exact: true };
        writeFamily(queryClient, family, exactQuery, mapItems, PATCH_POLICY);
      }
      if (query.state.fetchStatus === 'idle') unsubscribe();
    });
  }
}
