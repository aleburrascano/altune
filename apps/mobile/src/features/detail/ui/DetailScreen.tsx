/**
 * DetailScreen — read-only detail for a tapped discovery result.
 *
 * Fed by the in-memory handoff (no per-item backend fetch). The header (back
 * affordance + hero artwork + title + artist·year) is shared across kinds; the
 * body differs per kind (track: play/save + nav; album: tracklist; artist:
 * popular tracks + discography). Identity genres sit under the header inside
 * each body; deep provider metadata lives behind a single Disclosure, not as
 * always-on slabs. An empty handoff redirects to /discover.
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

import { trackExtras } from '../extras-accessors';
import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDetailEnrichments } from '../hooks/useDetailEnrichments';
import { useEnrichResult } from '../hooks/useEnrichResult';
import { useLateralNav } from '../hooks/useLateralNav';

import { _albumYear } from './helpers';
import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';
import { Disclosure } from './Disclosure';
import { DiscogsArtistSection } from './DiscogsArtistSection';
import { DeezerEnrichmentSection } from './DeezerEnrichmentSection';
import { DiscogsEnrichmentSection } from './DiscogsEnrichmentSection';
import { LastFmEnrichmentSection } from './LastFmEnrichmentSection';

const HERO_SIZE = 200;

function _headerYear(
  result: ReturnType<typeof getDetailHandoff>,
  mbYear: number,
): string | null {
  if (result === null) {
    return null;
  }
  if (mbYear > 0) {
    return String(mbYear);
  }
  if (result.kind === 'album') {
    return _albumYear(result);
  }
  if (result.kind === 'track') {
    const y = trackExtras(result.extras).year;
    return y != null ? String(y) : null;
  }
  return null;
}

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const segments = useSegments();
  const tabRoot = segments[1] === 'library' ? 'library' : 'discover';
  const detailRoute = `/${tabRoot}/detail` as const;
  const rawResult = getDetailHandoff();
  const { enriched: result } = useEnrichResult(rawResult ?? { kind: 'track', title: '', subtitle: null, image_url: null, confidence: 'low', sources: [], extras: {} });
  const lateralNav = useLateralNav();

  const isFromLibrary = (rawResult?.sources.length ?? 0) === 0;
  const isArtist = result.kind === 'artist';
  const isLibraryArtist = isArtist && isFromLibrary;
  const artistDiscovery = useArtistDiscovery({
    artistName: result.title,
    enabled: isLibraryArtist,
  });
  // All provider enrichments, fetched behind one seam. The resolved MusicBrainz
  // HD artwork wins for the hero; its genres become the identity pill row and
  // its year the header subtitle; the deep cuts live behind a Disclosure.
  const enrichments = useDetailEnrichments(result);

  // All hooks are called above; only now is it safe to bail on an empty
  // handoff (cold start / reload / deep link) — returning earlier would make
  // useArtistDiscovery/useDetailEnrichments conditional and break hook order.
  if (rawResult === null) {
    return <Redirect href="/discover" />;
  }

  const heroImageUrl =
    (enrichments.musicbrainz?.artwork_url ?? '') !== ''
      ? enrichments.musicbrainz!.artwork_url
      : isLibraryArtist && artistDiscovery.imageUrl != null
        ? artistDiscovery.imageUrl
        : result.image_url;

  const genres = enrichments.musicbrainz?.genres ?? [];
  const yearLabel = _headerYear(rawResult, enrichments.musicbrainz?.year ?? 0);
  const canNavToArtist = result.subtitle !== null && result.kind !== 'artist';

  const onArtistPress = (): void => {
    if (canNavToArtist && result.subtitle !== null) {
      void lateralNav.navigateTo(result.subtitle, 'artist');
    }
  };

  const albumHasDetails = enrichments.discogsAlbum !== null || enrichments.deezer !== null;
  const artistHasAbout = enrichments.lastfm !== null || enrichments.discogsArtist !== null;

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
      {result.subtitle !== null || yearLabel !== null ? (
        <View style={styles.subRow}>
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
          {yearLabel !== null ? (
            <Text variant="body" tone="tertiary">
              {result.subtitle !== null ? `  ·  ${yearLabel}` : yearLabel}
            </Text>
          ) : null}
        </View>
      ) : null}
    </View>
  );

  return (
    <Screen testID="detail-header">
      {backButton}
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scrollContent}
      >
        {heroContent}

        {result.kind === 'track' ? (
          <TrackDetailBody result={result} lateralNav={lateralNav} detailRoute={detailRoute} genres={genres} />
        ) : null}

        {result.kind === 'album' ? (
          <AlbumDetailBody result={result} detailRoute={detailRoute} isFromLibrary={isFromLibrary} genres={genres} />
        ) : null}
        {result.kind === 'album' && albumHasDetails ? (
          <Disclosure label="Details & credits" testID="detail-album-details">
            <DiscogsEnrichmentSection enrichment={enrichments.discogsAlbum} />
            <DeezerEnrichmentSection kind="album" enrichment={enrichments.deezer} />
          </Disclosure>
        ) : null}

        {result.kind === 'artist' ? (
          <ArtistDetailBody result={result} detailRoute={detailRoute} isFromLibrary={isFromLibrary} genres={genres} />
        ) : null}
        {result.kind === 'artist' && artistHasAbout ? (
          <Disclosure label={`About ${result.title}`} testID="detail-artist-about">
            <LastFmEnrichmentSection kind="artist" enrichment={enrichments.lastfm} />
            <DiscogsArtistSection enrichment={enrichments.discogsArtist} />
          </Disclosure>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scrollContent: { paddingBottom: 140 },
  back: { paddingVertical: spacing.lg, alignSelf: 'flex-start', minHeight: 48 },
  hero: { alignItems: 'center', paddingTop: spacing.lg, gap: spacing.sm },
  title: { textAlign: 'center', marginTop: spacing.lg },
  subRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', flexWrap: 'wrap' },
});
