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

import { Redirect, useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { getDetailHandoff } from '@shared/lib/detail-handoff';

const HERO_SIZE = 200;

function _kindLabel(kind: 'artist' | 'album' | 'track'): string {
  if (kind === 'artist') {
    return 'Artist';
  }
  return kind === 'album' ? 'Album' : 'Song';
}

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const result = getDetailHandoff();

  if (result === null) {
    return <Redirect href="/discover" />;
  }

  const isArtist = result.kind === 'artist';

  return (
    <Screen testID="detail-header">
      <Pressable
        testID="detail-back"
        onPress={() => router.back()}
        style={({ pressed }) => [styles.back, pressed ? { opacity: 0.6 } : null]}
      >
        <Text variant="label" tone="accent">
          ‹ Back
        </Text>
      </Pressable>

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
          <Text variant="body" tone="secondary" numberOfLines={1}>
            {result.subtitle}
          </Text>
        ) : null}
        <Text variant="label" tone="tertiary" style={styles.kind}>
          {_kindLabel(result.kind)}
        </Text>
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  back: { paddingVertical: spacing.md, alignSelf: 'flex-start' },
  hero: { alignItems: 'center', paddingTop: spacing.lg, gap: spacing.sm },
  title: { textAlign: 'center', marginTop: spacing.lg },
  kind: { marginTop: spacing.xs },
});
