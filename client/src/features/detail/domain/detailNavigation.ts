import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { FeaturedArtist } from '@shared/apiClient/trackResponse';
import { detailHref } from '@shared/navigation/detailHandoff';
import type { Href, Navigator, TabRoot } from '@shared/navigation';
import type { UnreleasedWhere } from '@features/detail/domain/unreleasedView';

export type DetailRoute = `/${TabRoot}/detail`;
type FeaturingRoute = `/${TabRoot}/featuring`;
type UnreleasedRoute = `/${TabRoot}/unreleased`;

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

function unreleasedRouteFor(detailRoute: DetailRoute): UnreleasedRoute {
  return detailRoute === '/library/detail' ? '/library/unreleased' : '/discover/unreleased';
}

export function unreleasedHref(
  detailRoute: DetailRoute,
  artist: DiscoveryResult,
  where: UnreleasedWhere,
): Href {
  const { pathname, params } = detailHref(unreleasedRouteFor(detailRoute), artist);
  return { pathname, params: { ...params, where } };
}
