/**
 * Detail navigation — the single seam for opening a detail screen.
 *
 * The invariant: a detail route is never pushed without the handoff written
 * first (DetailScreen redirects to /discover on an empty handoff). openDetail
 * is the only place that pairs the two. Routes are typed unions derived from
 * the tab root, replacing the per-call-site `as '/discover/detail'` casts and
 * the `.replace('/detail', '/featuring')` string surgery.
 */

import type { Router } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

export type TabRoot = 'discover' | 'library';
export type DetailRoute = `/${TabRoot}/detail`;
export type FeaturingRoute = `/${TabRoot}/featuring`;

/** The tab stack this screen lives in: segments[1] under the (tabs) group. */
export function tabRootFromSegments(segments: string[]): TabRoot {
  return segments[1] === 'library' ? 'library' : 'discover';
}

export function detailRouteFor(tabRoot: TabRoot): DetailRoute {
  return `/${tabRoot}/detail`;
}

/** The featuring browse route in the same tab stack as a detail route. */
export function featuringRouteFor(detailRoute: DetailRoute): FeaturingRoute {
  return detailRoute === '/library/detail' ? '/library/featuring' : '/discover/featuring';
}

/**
 * Write the handoff, then push — the only sanctioned way to open a detail
 * screen. Pushing (not replacing) builds the back stack for chained
 * detail → detail navigation.
 */
export function openDetail(
  router: Router,
  detailRoute: DetailRoute,
  result: DiscoveryResult,
): void {
  setDetailHandoff(result);
  router.push(detailRoute);
}
