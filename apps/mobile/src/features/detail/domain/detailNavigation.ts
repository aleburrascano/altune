import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';
import { detailHref } from '@shared/lib/detail-handoff';
import type { Href, Navigator, TabRoot } from '@shared/navigation';

export type DetailRoute = `/${TabRoot}/detail`;
export type FeaturingRoute = `/${TabRoot}/featuring`;

export function detailRouteFor(tabRoot: TabRoot): DetailRoute {
  return `/${tabRoot}/detail`;
}

export function featuringRouteFor(detailRoute: DetailRoute): FeaturingRoute {
  return detailRoute === '/library/detail' ? '/library/featuring' : '/discover/featuring';
}

export function openDetail(
  navigator: Navigator,
  detailRoute: DetailRoute,
  result: DiscoveryResult,
): void {
  navigator.push(detailHref(detailRoute, result));
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
