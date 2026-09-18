import { Redirect, useLocalSearchParams, useRouter, useSegments } from 'expo-router';
import type { ReactElement } from 'react';

import { readDetailHandoff, type DetailHandoff } from '@shared/lib/detail-handoff';
import { featuredArtistsFromExtras } from '@shared/lib/featured';

import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDetailEnrichments } from '../hooks/useDetailEnrichments';
import { useResolveMissingSources } from '../hooks/useResolveMissingSources';
import { useLateralNav } from '../hooks/useLateralNav';
import { DetailHandoffProvider } from '../handoff-context';
import { detailRouteFor, tabRootFromSegments } from '../navigation';

import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';
import type { DetailChrome } from './DetailScaffold';
import { secondaryLine } from './secondaryLine';

export function DetailScreen(): ReactElement {
  const params = useLocalSearchParams<{ handoff?: string }>();
  const handoff = readDetailHandoff(params.handoff);

  if (handoff === null) {
    return <Redirect href="/discover" />;
  }

  return (
    <DetailHandoffProvider value={handoff}>
      <DetailContent handoff={handoff} />
    </DetailHandoffProvider>
  );
}

function DetailContent({ handoff }: { handoff: DetailHandoff }): ReactElement {
  const router = useRouter();
  const segments = useSegments();
  const tabRoot = tabRootFromSegments(segments);
  const detailRoute = detailRouteFor(tabRoot);
  const rawResult = handoff.result;
  const { resolved: result } = useResolveMissingSources(rawResult);
  const lateralNav = useLateralNav();

  const isArtist = result.kind === 'artist';
  // Keyed off the handoff, not the resolved result: backfilling sources brings an
  // artist no artwork, so a library-originated artist still needs the image search.
  const isLibraryArtist = isArtist && rawResult.sources.length === 0;
  const artistDiscovery = useArtistDiscovery({
    artistName: result.title,
    enabled: isLibraryArtist,
  });
  const enrichments = useDetailEnrichments(result);

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
