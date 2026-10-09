import { Redirect, useLocalSearchParams, useRouter, useSegments } from 'expo-router';
import type { ReactElement } from 'react';

import { readDetailHandoff, type DetailHandoff } from '@shared/navigation/detailHandoff';
import { featuredArtistsFromExtras } from '@shared/api-client/featuredArtists';
import { tabRootFromSegments } from '@shared/navigation';

import { useArtistDiscovery } from '@features/detail/hooks/useEntityDiscovery';
import { useDetailEnrichments } from '@features/detail/hooks/useDetailEnrichments';
import { useResolveMissingSources } from '@features/detail/hooks/useResolveMissingSources';
import { useLateralNav } from '@features/detail/hooks/useDetailNavigation';
import { DetailHandoffProvider } from '@features/detail/hooks/useDetailHandoff';
import { detailRouteFor } from '@features/detail/domain/detailNavigation';

import { TrackDetailBody } from '@features/detail/ui/TrackDetailBody';
import { AlbumDetailBody } from '@features/detail/ui/AlbumDetailBody';
import { ArtistDetailBody } from '@features/detail/ui/ArtistDetailBody';
import type { DetailChrome } from '@features/detail/ui/DetailScaffold';
import { secondaryLine } from '@features/detail/ui/secondaryLine';

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
  const lateralNav = useLateralNav(detailRoute);

  const isArtist = result.kind === 'artist';
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
        lastfmError={enrichments.errors.lastfm}
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
