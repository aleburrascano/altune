/**
 * DetailScreen — read-only detail for a tapped discovery result.
 *
 * Fed by the in-memory handoff (no per-item backend fetch). The header (back
 * affordance + hero artwork + title/subtitle/kind) is shared across kinds;
 * the body differs per kind (track info rows + Save; album/artist placeholders)
 * and is filled in by later slices. An empty handoff redirects to /discover.
 *
 * Primitives are imported directly (not via the @shared/ui barrel) so jest
 * component tests don't transitively load unrelated native modules; Artwork's
 * expo-image dependency is mocked in the test.
 */

import { Redirect, useRouter, useSegments } from 'expo-router';
import type { ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { getDetailHandoff } from '@shared/lib/detail-handoff';

import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDeezerEnrichment } from '../hooks/useDeezerEnrichment';
import { useDiscogsArtistEnrichment } from '../hooks/useDiscogsArtistEnrichment';
import { useDiscogsEnrichment } from '../hooks/useDiscogsEnrichment';
import { useEnrichment } from '../hooks/useEnrichment';
import { useEnrichResult } from '../hooks/useEnrichResult';
import { useLastFmEnrichment } from '../hooks/useLastFmEnrichment';
import { useLateralNav } from '../hooks/useLateralNav';
import { useLyrics } from '../hooks/useLyrics';

import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';
import { DiscogsArtistSection } from './DiscogsArtistSection';
import { DeezerEnrichmentSection } from './DeezerEnrichmentSection';
import { DiscogsEnrichmentSection } from './DiscogsEnrichmentSection';
import { EnrichmentSection } from './EnrichmentSection';
import { LastFmEnrichmentSection } from './LastFmEnrichmentSection';
import { LyricsSection } from './LyricsSection';

const HERO_SIZE = 200;

function _kindLabel(kind: 'artist' | 'album' | 'track'): string {
  if (kind === 'artist') {
    return 'Artist';
  }
  return kind === 'album' ? 'Album' : 'Track';
}

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const segments = useSegments();
  const tabRoot = segments[1] === 'library' ? 'library' : 'discover';
  const detailRoute = `/${tabRoot}/detail` as const;
  const rawResult = getDetailHandoff();
  const { enriched: result } = useEnrichResult(rawResult ?? { kind: 'track', title: '', subtitle: null, image_url: null, confidence: 'low', sources: [], extras: {} });
  const lateralNav = useLateralNav();

  if (rawResult === null) {
    return <Redirect href="/discover" />;
  }

  const isFromLibrary = rawResult.sources.length === 0;
  const isArtist = result.kind === 'artist';
  const isLibraryArtist = isArtist && isFromLibrary;
  const artistDiscovery = useArtistDiscovery({
    artistName: result.title,
    enabled: isLibraryArtist,
  });
  // MusicBrainz detail enrichment (genres/year/rating + HD cover). The resolved
  // HD artwork wins for the hero; metadata renders in EnrichmentSection below.
  const mbid = typeof result.extras.mbid === 'string' ? result.extras.mbid : undefined;
  const { enrichment } = useEnrichment({
    kind: result.kind,
    title: result.title,
    subtitle: result.subtitle,
    mbid,
  });
  // Discogs liner-notes enrichment — album-scoped (credits hang off the master).
  const { enrichment: discogsEnrichment } = useDiscogsEnrichment({
    album: result.title,
    artist: result.subtitle,
    enabled: result.kind === 'album',
  });
  // Discogs artist enrichment — bio / aliases / groups / links (cap 7).
  const { enrichment: discogsArtist } = useDiscogsArtistEnrichment({
    name: result.title,
    enabled: result.kind === 'artist',
  });
  // Last.fm enrichment — listen popularity, tags, similar artists, bio/blurb.
  const { enrichment: lastfmEnrichment } = useLastFmEnrichment({
    kind: result.kind,
    title: result.title,
    subtitle: result.subtitle,
  });
  // Deezer enrichment — track tempo/explicit + album label/genres (caps 7–8).
  const { enrichment: deezerEnrichment } = useDeezerEnrichment({
    kind: result.kind,
    title: result.title,
    subtitle: result.subtitle,
    enabled: result.kind === 'track' || result.kind === 'album',
  });
  // Deezer lyrics — synced + plain lyrics, writers, copyright (cap 6). Track-only.
  const { lyrics } = useLyrics({
    title: result.title,
    subtitle: result.subtitle,
    enabled: result.kind === 'track',
  });

  const heroImageUrl =
    (enrichment?.artwork_url ?? '') !== ''
      ? enrichment!.artwork_url
      : isLibraryArtist && artistDiscovery.imageUrl != null
        ? artistDiscovery.imageUrl
        : result.image_url;

  const canNavToArtist = result.subtitle !== null && result.kind !== 'artist';

  const onArtistPress = (): void => {
    if (canNavToArtist && result.subtitle !== null) {
      void lateralNav.navigateTo(result.subtitle, 'artist');
    }
  };

  const backButton = (
    <Pressable
      testID="detail-back"
      onPress={() => {
        if (router.canGoBack()) {
          router.back();
        } else {
          router.replace(`/${tabRoot}` as '/discover' | '/library');
        }
      }}
      accessibilityRole="button"
      accessibilityLabel="Go back"
      style={({ pressed }) => [styles.back, pressed ? { opacity: 0.6 } : null]}
    >
      <Text variant="label" tone="accent">
        ‹ Back
      </Text>
    </Pressable>
  );

  const heroContent = (
    <>
      <View style={styles.hero}>
        <Artwork
          uri={heroImageUrl}
          size={HERO_SIZE}
          radius={isArtist ? radius.full : radius.lg}
          accessibilityLabel={result.title}
        />
        <Text variant="displayL" style={styles.title} numberOfLines={2}>
          {result.title}
        </Text>
        {result.subtitle !== null ? (
          canNavToArtist ? (
            <Pressable
              testID="detail-artist-link"
              onPress={onArtistPress}
              disabled={lateralNav.state === 'searching'}
              accessibilityRole="link"
              accessibilityLabel={`View artist ${result.subtitle}`}
              accessibilityHint="Opens artist detail"
              style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
            >
              <Text variant="body" tone="accent" numberOfLines={1}>
                {result.subtitle}
              </Text>
            </Pressable>
          ) : (
            <Text variant="body" tone="secondary" numberOfLines={1}>
              {result.subtitle}
            </Text>
          )
        ) : null}
        <Text variant="label" tone="tertiary" style={styles.kind}>
          {_kindLabel(result.kind)}
        </Text>
      </View>
    </>
  );

  return (
    <Screen testID="detail-header">
      {backButton}
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scrollContent}
      >
        {heroContent}
        <EnrichmentSection enrichment={enrichment} />
        <LastFmEnrichmentSection kind={result.kind} enrichment={lastfmEnrichment} />
        {result.kind === 'track' ? <TrackDetailBody result={result} lateralNav={lateralNav} detailRoute={detailRoute} /> : null}
        {result.kind === 'album' ? <AlbumDetailBody result={result} detailRoute={detailRoute} isFromLibrary={isFromLibrary} /> : null}
        {result.kind === 'album' ? <DiscogsEnrichmentSection enrichment={discogsEnrichment} /> : null}
        {result.kind === 'track' || result.kind === 'album' ? (
          <DeezerEnrichmentSection kind={result.kind} enrichment={deezerEnrichment} />
        ) : null}
        {result.kind === 'track' ? <LyricsSection lyrics={lyrics} /> : null}
        {result.kind === 'artist' ? <ArtistDetailBody result={result} detailRoute={detailRoute} isFromLibrary={isFromLibrary} /> : null}
        {result.kind === 'artist' ? <DiscogsArtistSection enrichment={discogsArtist} /> : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scrollContent: { paddingBottom: 140 },
  back: { paddingVertical: spacing.lg, alignSelf: 'flex-start', minHeight: 48 },
  hero: { alignItems: 'center', paddingTop: spacing.lg, gap: spacing.sm },
  title: { textAlign: 'center', marginTop: spacing.lg },
  kind: { marginTop: spacing.xs },
});
