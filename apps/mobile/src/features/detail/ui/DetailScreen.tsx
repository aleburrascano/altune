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

import { useEnrichResult } from '../hooks/useEnrichResult';
import { useLateralNav } from '../hooks/useLateralNav';

import { TrackDetailBody } from './TrackDetailBody';
import { AlbumDetailBody } from './AlbumDetailBody';
import { ArtistDetailBody } from './ArtistDetailBody';

const HERO_SIZE = 200;

function _kindLabel(kind: 'artist' | 'album' | 'track'): string {
  if (kind === 'artist') {
    return 'Artist';
  }
  return kind === 'album' ? 'Album' : 'Song';
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

  const isArtist = result.kind === 'artist';
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
          uri={result.image_url}
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
        {result.kind === 'track' ? <TrackDetailBody result={result} lateralNav={lateralNav} /> : null}
        {result.kind === 'album' ? <AlbumDetailBody result={result} detailRoute={detailRoute} /> : null}
        {result.kind === 'artist' ? <ArtistDetailBody result={result} detailRoute={detailRoute} /> : null}
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
