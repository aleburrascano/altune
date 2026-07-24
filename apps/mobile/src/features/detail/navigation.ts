import type { Router } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

export type TabRoot = 'discover' | 'library';
export type DetailRoute = `/${TabRoot}/detail`;
export type FeaturingRoute = `/${TabRoot}/featuring`;

export function tabRootFromSegments(segments: string[]): TabRoot {
  return segments[1] === 'library' ? 'library' : 'discover';
}

export function detailRouteFor(tabRoot: TabRoot): DetailRoute {
  return `/${tabRoot}/detail`;
}

export function featuringRouteFor(detailRoute: DetailRoute): FeaturingRoute {
  return detailRoute === '/library/detail' ? '/library/featuring' : '/discover/featuring';
}

export function openDetail(
  router: Router,
  detailRoute: DetailRoute,
  result: DiscoveryResult,
): void {
  setDetailHandoff(result);
  router.push(detailRoute);
}
