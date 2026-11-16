import type { CacheFilters, EventCache } from '@shared/events/core/eventCache';
import {
  updaterFor,
  type MapItems,
  type TrackCacheFamily,
  type TrackCacheValue,
  type WritePolicy,
} from '@shared/events/core/trackCacheShapes';

export function writeFamily(
  cache: EventCache,
  family: TrackCacheFamily,
  filters: CacheFilters,
  mapItems: MapItems,
  policy: WritePolicy,
): void {
  cache.setQueriesData<TrackCacheValue>(filters, updaterFor(family.shape, mapItems, policy));
}
