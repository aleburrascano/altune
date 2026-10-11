import { Redirect, useLocalSearchParams, useRouter, useSegments } from 'expo-router';
import type { ReactElement } from 'react';

import { readDetailHandoff, type DetailHandoff } from '@shared/navigation/detailHandoff';
import { featuredArtistsFromExtras } from '@shared/apiClient/featuredArtists';
import { tabRootFromSegments } from '@shared/navigation';

import { useArtistDiscovery } from '@features/detail/hooks/useEntityDiscovery';
import { realPhotoUrl } from '@features/detail/domain/artistPhoto';
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
  const { resolved: entity, isResolving } = useResolveMissingSources(rawResult);
  const lateralNav = useLateralNav(detailRoute);

  const isArtist = entity.kind === 'artist';
  const isLibraryArtist = isArtist && rawResult.sources.length === 0;
  const artistDiscovery = useArtistDiscovery({
    artistName: entity.title,
    enabled: isLibraryArtist && !isResolving && entity.sources.length === 0,
  });
  const enrichments = useDetailEnrichments(entity);

  const searchedPhoto = isLibraryArtist ? artistDiscovery.imageUrl : null;
  const artworkUrl = realPhotoUrl(entity.image_url) ?? searchedPhoto;

  const albumCollaborators = featuredArtistsFromExtras(
    entity.kind === 'album' ? enrichments.deezer?.featured_artists : null,
  );

  const chrome: DetailChrome = {
    title: entity.title,
    artworkUrl,
    secondary: secondaryLine({
      isArtist,
      genreTags: enrichments.lastfm?.tags,
      artist: entity.subtitle,
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

  if (entity.kind === 'album') {
    return (
      <AlbumDetailBody
        chrome={chrome}
        result={entity}
        detailRoute={detailRoute}
        mbYear={enrichments.musicbrainz?.year ?? 0}
      />
    );
  }

  if (entity.kind === 'artist') {
    return (
      <ArtistDetailBody
        chrome={chrome}
        result={entity}
        detailRoute={detailRoute}
        lastfm={enrichments.lastfm}
        lastfmError={enrichments.errors.lastfm}
      />
    );
  }

  return (
    <TrackDetailBody
      chrome={chrome}
      result={entity}
      lateralNav={lateralNav}
      detailRoute={detailRoute}
      deezerFeatured={featuredArtistsFromExtras(enrichments.deezer?.featured_artists)}
      mbYear={enrichments.musicbrainz?.year ?? 0}
    />
  );
}
