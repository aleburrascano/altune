import type { TrackId, AcquisitionTransition, TrackFields, TrackResponse } from '@shared/apiClient';

import type {
  CacheChange,
  CachedQueries,
  CachedQuery,
  CacheFilters,
  CacheKey,
  EventCache,
} from '@shared/events/core/eventCache';
import {
  PATCH_POLICY,
  TRACK_CACHE_FAMILY_LIST,
  type MapItems,
  type TrackCacheFamily,
} from '@shared/events/core/trackCacheShapes';
import { writeFamily } from '@shared/events/shell/writeFamily';

export type TrackPatch = Partial<TrackFields> &
  (
    | AcquisitionTransition
    | { acquisition_status?: never; failure_reason?: never; failure_message?: never }
  );

export function withPatch(track: TrackResponse, patch: TrackPatch | undefined): TrackResponse {
  return patch ? { ...track, ...patch } : track;
}

interface PendingTrackPatches {
  readonly cache: EventCache;
  readonly byTrackId: Map<TrackId, TrackPatch>;
}

let pendingPatches: PendingTrackPatches | null = null;

function openBatch(cache: EventCache): PendingTrackPatches {
  if (pendingPatches && pendingPatches.cache !== cache) flushTrackCachePatches();
  if (pendingPatches === null) {
    pendingPatches = { cache, byTrackId: new Map() };
    void Promise.resolve().then(flushTrackCachePatches);
  }
  return pendingPatches;
}

export function scheduleTrackPatch(cache: EventCache, trackId: TrackId, patch: TrackPatch): void {
  const { byTrackId } = openBatch(cache);
  const prior = byTrackId.get(trackId);
  byTrackId.set(trackId, prior ? { ...prior, ...patch } : patch);
}

export function pendingPatchFor(cache: EventCache, trackId: TrackId): TrackPatch | undefined {
  return pendingPatches?.cache === cache ? pendingPatches.byTrackId.get(trackId) : undefined;
}

function patchesOf(batch: PendingTrackPatches): MapItems {
  return (items) => items.map((t) => withPatch(t, batch.byTrackId.get(t.id)));
}

export function flushTrackCachePatches(): void {
  const batch = pendingPatches;
  if (batch === null) return;
  pendingPatches = null;
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    flushFamily(batch, family);
  }
}

function flushFamily(batch: PendingTrackPatches, family: TrackCacheFamily): void {
  const { cache } = batch;
  const mapItems = patchesOf(batch);
  writeFamily(cache, family, { queryKey: family.prefix }, mapItems, PATCH_POLICY);
  const replay = exactWriter(cache, family, mapItems);
  replayOverInFlightFetches(cache.getQueryCache(), family.prefix, replay);
}

export function patchTrackInCaches(cache: EventCache, trackId: TrackId, patch: TrackPatch): void {
  scheduleTrackPatch(cache, trackId, patch);
  flushTrackCachePatches();
}

function exactKey(query: CachedQuery): CacheFilters {
  return { queryKey: query.queryKey, exact: true };
}

function exactWriter(
  cache: EventCache,
  family: TrackCacheFamily,
  mapItems: MapItems,
): (query: CachedQuery) => void {
  return (query) => writeFamily(cache, family, exactKey(query), mapItems, PATCH_POLICY);
}

function isFreshSuccess(change: CacheChange): boolean {
  return change.type === 'updated' && change.action.type === 'success' && !change.action.manual;
}

function isSettled(change: CacheChange, query: CachedQuery): boolean {
  return change.type === 'updated' && query.state.fetchStatus === 'idle';
}

function onFreshSuccess(queries: CachedQueries, query: CachedQuery, replay: () => void): void {
  const unsubscribe = queries.subscribe((change) => {
    if (change.query !== query) return;
    if (change.type === 'removed') return unsubscribe();
    if (isFreshSuccess(change)) replay();
    if (isSettled(change, query)) unsubscribe();
  });
}

function replayOverInFlightFetches(
  queries: CachedQueries,
  prefix: CacheKey,
  replay: (query: CachedQuery) => void,
): void {
  for (const query of queries.findAll({ queryKey: prefix })) {
    if (query.state.fetchStatus !== 'idle') onFreshSuccess(queries, query, () => replay(query));
  }
}
