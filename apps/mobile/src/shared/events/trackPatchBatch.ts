import type {
  Query,
  QueryCache,
  QueryCacheNotifyEvent,
  QueryClient,
  QueryFilters,
  QueryKey,
} from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionTransition } from '@shared/api-client/trackAcquisition';
import type { TrackFields, TrackResponse } from '@shared/api-client/trackResponse';
import {
  PATCH_POLICY,
  TRACK_CACHE_FAMILY_LIST,
  writeFamily,
  type MapItems,
  type TrackCacheFamily,
} from '@shared/events/trackCacheShapes';

export type TrackPatch = Partial<TrackFields> &
  (
    | AcquisitionTransition
    | { acquisition_status?: never; failure_reason?: never; failure_message?: never }
  );

export function withPatch(track: TrackResponse, patch: TrackPatch | undefined): TrackResponse {
  return patch ? { ...track, ...patch } : track;
}

interface PendingTrackPatches {
  readonly queryClient: QueryClient;
  readonly byTrackId: Map<TrackId, TrackPatch>;
}

let pendingPatches: PendingTrackPatches | null = null;

function openBatch(queryClient: QueryClient): PendingTrackPatches {
  if (pendingPatches && pendingPatches.queryClient !== queryClient) flushTrackCachePatches();
  if (pendingPatches === null) {
    pendingPatches = { queryClient, byTrackId: new Map() };
    void Promise.resolve().then(flushTrackCachePatches);
  }
  return pendingPatches;
}

export function scheduleTrackPatch(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  const { byTrackId } = openBatch(queryClient);
  const prior = byTrackId.get(trackId);
  byTrackId.set(trackId, prior ? { ...prior, ...patch } : patch);
}

export function pendingPatchFor(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackPatch | undefined {
  return pendingPatches?.queryClient === queryClient
    ? pendingPatches.byTrackId.get(trackId)
    : undefined;
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
  const { queryClient } = batch;
  const mapItems = patchesOf(batch);
  writeFamily(queryClient, family, { queryKey: family.prefix }, mapItems, PATCH_POLICY);
  const replay = exactWriter(queryClient, family, mapItems);
  replayOverInFlightFetches(queryClient.getQueryCache(), family.prefix, replay);
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  scheduleTrackPatch(queryClient, trackId, patch);
  flushTrackCachePatches();
}

function exactKey(query: Query): QueryFilters {
  return { queryKey: query.queryKey, exact: true };
}

function exactWriter(
  queryClient: QueryClient,
  family: TrackCacheFamily,
  mapItems: MapItems,
): (query: Query) => void {
  return (query) => writeFamily(queryClient, family, exactKey(query), mapItems, PATCH_POLICY);
}

function isFreshSuccess(event: QueryCacheNotifyEvent): boolean {
  return event.type === 'updated' && event.action.type === 'success' && !event.action.manual;
}

function onFreshSuccess(cache: QueryCache, query: Query, replay: () => void): void {
  const unsubscribe = cache.subscribe((event) => {
    if (event.query !== query) return;
    if (event.type === 'removed') return unsubscribe();
    if (isFreshSuccess(event)) replay();
    if (event.type === 'updated' && query.state.fetchStatus === 'idle') unsubscribe();
  });
}

function replayOverInFlightFetches(
  cache: QueryCache,
  prefix: QueryKey,
  replay: (query: Query) => void,
): void {
  for (const query of cache.findAll({ queryKey: prefix })) {
    if (query.state.fetchStatus !== 'idle') onFreshSuccess(cache, query, () => replay(query));
  }
}
