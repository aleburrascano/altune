import { useRouter, type Href, type ImperativeRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';
import { detailHref } from '@shared/lib/detail-handoff';

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
  router: ImperativeRouter,
  detailRoute: DetailRoute,
  result: DiscoveryResult,
): void {
  router.push(detailHref(detailRoute, result));
}

export function useOpenDetail(detailRoute: DetailRoute): (picked: DiscoveryResult) => void {
  const router = useRouter();
  return (picked) => openDetail(router, detailRoute, picked);
}

export function featuringHref(detailRoute: DetailRoute, artist: FeaturedArtist): Href {
  return {
    pathname: featuringRouteFor(detailRoute),
    params: {
      name: artist.name,
      ...(artist.mbid ? { mbid: artist.mbid } : {}),
      ...(artist.deezer_id != null ? { deezer_id: String(artist.deezer_id) } : {}),
    },
  };
}

export function useOpenFeaturing(detailRoute: DetailRoute): (artist: FeaturedArtist) => void {
  const router = useRouter();
  return (artist) => router.push(featuringHref(detailRoute, artist));
}
