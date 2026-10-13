import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { FeaturedArtist } from '@shared/apiClient/trackResponse';
import { detailHref } from '@shared/navigation/detailHandoff';
import type { Href, Navigator, TabRoot } from '@shared/navigation';

export type DetailRoute = `/${TabRoot}/detail`;
type FeaturingRoute = `/${TabRoot}/featuring`;

export function detailRouteFor(tabRoot: TabRoot): DetailRoute {
  return `/${tabRoot}/detail`;
}

function featuringRouteFor(detailRoute: DetailRoute): FeaturingRoute {
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
