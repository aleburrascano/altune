import type { QueryClient } from '@tanstack/react-query';

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

export function pendingPatchFor(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackPatch | undefined {
  return pendingPatches?.queryClient === queryClient
    ? pendingPatches.byTrackId.get(trackId)
    : undefined;
}

export function flushTrackCachePatches(): void {
  const batch = pendingPatches;
  if (batch === null) return;
  pendingPatches = null;
  const applyBatch: MapItems = (items) => items.map((t) => withPatch(t, batch.byTrackId.get(t.id)));
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(batch.queryClient, family, { queryKey: family.prefix }, applyBatch, PATCH_POLICY);
    replayOverInFlightFetches(batch.queryClient, family, applyBatch);
  }
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  scheduleTrackPatch(queryClient, trackId, patch);
  flushTrackCachePatches();
}

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
      if (event.type !== 'updated') return;
      if (event.action.type === 'success' && !event.action.manual) {
        const exactQuery = { queryKey: query.queryKey, exact: true };
        writeFamily(queryClient, family, exactQuery, mapItems, PATCH_POLICY);
      }
      if (query.state.fetchStatus === 'idle') unsubscribe();
    });
  }
}
