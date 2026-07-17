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
import { ChevronLeft } from 'lucide-react-native';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { getDetailHandoff } from '@shared/lib/detail-handoff';
import { featuredArtistsFromExtras, withFeaturing } from '@shared/lib/featured';

import { trackExtras } from '../extras-accessors';
import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useDetailEnrichments } from '../hooks/useDetailEnrichments';
import { useEnrichResult } from '../hooks/useEnrichResult';
import { useLateralNav } from '../hooks/useLateralNav';
import { detailRouteFor, tabRootFromSegments } from '../navigation';

import { _albumYear } from './helpers';
import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';
import { Disclosure } from './Disclosure';
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
  const tabRoot = tabRootFromSegments(segments);
  const detailRoute = detailRouteFor(tabRoot);
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
  // The rendered provider enrichments, fetched behind one seam: the
  // MusicBrainz year for the header, Deezer featured_artists for the collab
  // line / Featuring row, and the Last.fm artist About block.
  const enrichments = useDetailEnrichments(result);

  // All hooks are called above; only now is it safe to bail on an empty
  // handoff (cold start / reload / deep link) — returning earlier would make
  // useArtistDiscovery/useDetailEnrichments conditional and break hook order.
  if (rawResult === null) {
    return <Redirect href="/discover" />;
  }

  // Lock the hero to the tapped card's artwork — the identity-bound image the
  // discovery card already resolved. Do NOT re-resolve via enrichment here: a
  // name-based artwork lookup on open was overwriting the card with a different
  // (often wrong same-name) artist's image and causing a visible flicker.
  const heroImageUrl =
    (result.image_url ?? '') !== ''
      ? result.image_url
      : isLibraryArtist && artistDiscovery.imageUrl != null
        ? artistDiscovery.imageUrl
        : result.image_url;

  const yearLabel = _headerYear(rawResult, enrichments.musicbrainz?.year ?? 0);
  const canNavToArtist = result.subtitle !== null && result.kind !== 'artist';

  const onArtistPress = (): void => {
    if (canNavToArtist && result.subtitle !== null) {
      void lateralNav.navigateTo(result.subtitle, 'artist');
    }
  };

  const artistHasAbout = enrichments.lastfm !== null;

  const backButton = (
    <View style={styles.back}>
      <IconButton
        testID="detail-back"
        icon={ChevronLeft}
        size={24}
        onPress={() => {
          if (router.canGoBack()) {
            router.back();
          } else {
            router.replace(`/${tabRoot}` as '/discover' | '/library');
          }
        }}
        accessibilityLabel="Go back"
      />
    </View>
  );

  // For a collab album, append its co-primary artists to the header artist line
  // ("Drake, 21 Savage"). The tap target stays the primary (result.subtitle);
  // tracks keep their own "Featuring" row, so only albums augment the header.
  const albumCollaborators =
    result.kind === 'album' ? featuredArtistsFromExtras(enrichments.deezer?.featured_artists) : [];
  const subtitleDisplay =
    result.subtitle !== null ? withFeaturing(result.subtitle, albumCollaborators) : null;

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
                  {subtitleDisplay}
                </Text>
              </Pressable>
            ) : (
              <Text variant="body" tone="secondary" numberOfLines={1}>
                {subtitleDisplay}
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
          <TrackDetailBody
            result={result}
            lateralNav={lateralNav}
            detailRoute={detailRoute}
            deezerFeatured={featuredArtistsFromExtras(enrichments.deezer?.featured_artists)}
          />
        ) : null}

        {result.kind === 'album' ? (
          <AlbumDetailBody result={result} detailRoute={detailRoute} isFromLibrary={isFromLibrary} />
        ) : null}

        {result.kind === 'artist' ? (
          <ArtistDetailBody result={result} detailRoute={detailRoute} isFromLibrary={isFromLibrary} />
        ) : null}
        {result.kind === 'artist' && artistHasAbout ? (
          <Disclosure label={`About ${result.title}`} testID="detail-artist-about">
            <LastFmEnrichmentSection kind="artist" enrichment={enrichments.lastfm} />
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
