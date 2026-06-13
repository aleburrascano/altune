import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Artwork, Card, Row, Text, radius, spacing } from '@shared/ui';

import type { DiscoveryResult } from '../../../shared/api-client/discovery';

export function TopResultCard({
  result,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const isArtist = result.kind === 'artist';
  const kindLabel = isArtist ? 'Artist' : result.kind === 'album' ? 'Album' : 'Song';
  return (
    <View style={styles.topResultWrap}>
      <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
        TOP RESULT
      </Text>
      <Pressable
        testID="discover-top-result"
        onPress={() => onPress(result, 0)}
        accessibilityRole="button"
        accessibilityLabel={`${result.title}${result.subtitle ? `, ${result.subtitle}` : ''}, ${kindLabel}`}
        style={({ pressed }) => (pressed ? { opacity: 0.85 } : null)}
      >
        <Card>
          <Row
            leading={
              <Artwork
                uri={result.image_url}
                size={88}
                radius={isArtist ? radius.full : radius.lg}
                accessibilityLabel={result.title}
              />
            }
          >
            <Text variant="title" numberOfLines={2}>
              {result.title}
            </Text>
            {result.subtitle !== null ? (
              <Text variant="body" tone="secondary" numberOfLines={1} style={{ marginTop: 2 }}>
                {result.subtitle}
              </Text>
            ) : null}
            <Text variant="caption" tone="tertiary" style={{ marginTop: spacing.sm }}>
              {kindLabel}
            </Text>
          </Row>
        </Card>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  topResultWrap: { marginBottom: spacing.lg },
  sectionHeader: { marginBottom: spacing.md, letterSpacing: 1 },
});
