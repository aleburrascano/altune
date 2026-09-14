import { Redirect, useRouter, useSegments } from 'expo-router';
import type { ReactElement } from 'react';

import { getDetailHandoff } from '@shared/lib/detail-handoff';
import { featuredArtistsFromExtras } from '@shared/lib/featured';

import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDetailEnrichments } from '../hooks/useDetailEnrichments';
import { useResolveMissingSources } from '../hooks/useResolveMissingSources';
import { useLateralNav } from '../hooks/useLateralNav';
import { detailRouteFor, tabRootFromSegments } from '../navigation';

import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';
import type { DetailChrome } from './DetailScaffold';
import { secondaryLine } from './secondaryLine';

const EMPTY_RESULT = {
  kind: 'track' as const,
  title: '',
  subtitle: null,
  image_url: null,
  confidence: 'low' as const,
  sources: [],
  extras: {},
};

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const segments = useSegments();
  const tabRoot = tabRootFromSegments(segments);
  const detailRoute = detailRouteFor(tabRoot);
  const rawResult = getDetailHandoff();
  const { resolved: result } = useResolveMissingSources(rawResult ?? EMPTY_RESULT);
  const lateralNav = useLateralNav();

  const isFromLibrary = (rawResult?.sources.length ?? 0) === 0;
  const isArtist = result.kind === 'artist';
  const isLibraryArtist = isArtist && isFromLibrary;
  const artistDiscovery = useArtistDiscovery({
    artistName: result.title,
    enabled: isLibraryArtist,
  });
  const enrichments = useDetailEnrichments(result);

  if (rawResult === null) {
    return <Redirect href="/discover" />;
  }

  const artworkUrl =
    (result.image_url ?? '') !== ''
      ? result.image_url
      : isLibraryArtist
        ? artistDiscovery.imageUrl
        : result.image_url;

  const albumCollaborators =
    result.kind === 'album' ? featuredArtistsFromExtras(enrichments.deezer?.featured_artists) : [];

  const chrome: DetailChrome = {
    title: result.title,
    artworkUrl,
    secondary: secondaryLine({
      isArtist,
      genreTags: enrichments.lastfm?.tags,
      artist: result.subtitle,
      albumCollaborators,
      lateralNav,
    }),
    onBack: () => {
      if (router.canGoBack()) {
        router.back();
      } else {
        router.replace(`/${tabRoot}`);
      }
    },
  };

  if (result.kind === 'album') {
    return (
      <AlbumDetailBody
        chrome={chrome}
        result={result}
        detailRoute={detailRoute}
        isFromLibrary={isFromLibrary}
        mbYear={enrichments.musicbrainz?.year ?? 0}
      />
    );
  }

  if (result.kind === 'artist') {
    return (
      <ArtistDetailBody
        chrome={chrome}
        result={result}
        detailRoute={detailRoute}
        isFromLibrary={isFromLibrary}
        lastfm={enrichments.lastfm}
      />
    );
  }

  return (
    <TrackDetailBody
      chrome={chrome}
      result={result}
      lateralNav={lateralNav}
      detailRoute={detailRoute}
      deezerFeatured={featuredArtistsFromExtras(enrichments.deezer?.featured_artists)}
      mbYear={enrichments.musicbrainz?.year ?? 0}
    />
  );
}
