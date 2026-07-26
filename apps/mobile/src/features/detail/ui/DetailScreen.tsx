import { Redirect, useRouter, useSegments } from 'expo-router';
import type { ReactElement, ReactNode } from 'react';
import { Pressable } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';

import { getDetailHandoff } from '@shared/lib/detail-handoff';
import { featuredArtistsFromExtras, withFeaturing } from '@shared/lib/featured';

import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDetailEnrichments } from '../hooks/useDetailEnrichments';
import { useEnrichResult } from '../hooks/useEnrichResult';
import { useLateralNav } from '../hooks/useLateralNav';
import { detailRouteFor, tabRootFromSegments } from '../navigation';

import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';
import type { DetailChrome } from './DetailScaffold';

const EMPTY_RESULT = {
  kind: 'track' as const,
  title: '',
  subtitle: null,
  image_url: null,
  confidence: 'low' as const,
  sources: [],
  extras: {},
};

const MAX_GENRES = 2;

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const segments = useSegments();
  const tabRoot = tabRootFromSegments(segments);
  const detailRoute = detailRouteFor(tabRoot);
  const rawResult = getDetailHandoff();
  const { enriched: result } = useEnrichResult(rawResult ?? EMPTY_RESULT);
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
    secondary: secondaryLine(),
    onBack: () => {
      if (router.canGoBack()) {
        router.back();
      } else {
        router.replace(`/${tabRoot}` as '/discover' | '/library');
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

  function secondaryLine(): ReactNode {
    if (isArtist) {
      const genres = (enrichments.lastfm?.tags ?? []).slice(0, MAX_GENRES);
      if (genres.length === 0) {
        return null;
      }
      return (
        <Text variant="body" tone="secondary" numberOfLines={1}>
          {genres.join(' · ')}
        </Text>
      );
    }

    const artist = result.subtitle;
    if (artist === null) {
      return null;
    }

    return (
      <Pressable
        testID="detail-artist-link"
        onPress={() => void lateralNav.navigateTo(artist, 'artist')}
        disabled={lateralNav.state === 'searching'}
        accessibilityRole="link"
        accessibilityLabel={`View artist ${artist}`}
        accessibilityHint="Opens artist detail"
        style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
      >
        <Text variant="body" tone="accent" numberOfLines={1}>
          {withFeaturing(artist, albumCollaborators)}
        </Text>
      </Pressable>
    );
  }
}
