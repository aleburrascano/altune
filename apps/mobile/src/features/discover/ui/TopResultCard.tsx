import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Artwork, Card, Row, Text, radius, spacing } from '@shared/ui';

import { kindLabel } from '../state';
import type { DiscoveryResult } from '@shared/api-client/discovery';

export function TopResultCard({
  result,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const isArtist = result.kind === 'artist';
  // "Song", matching every other surface — this card used to say "Track",
  // the one visible drift the four hand-written kind→copy maps produced.
  const label = kindLabel(result.kind);
  return (
    <View style={styles.topResultWrap}>
      <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
        TOP RESULT
      </Text>
      <Pressable
        testID="discover-top-result"
        onPress={() => onPress(result, 0)}
        accessibilityRole="button"
        accessibilityLabel={`${result.title}${result.subtitle ? `, ${result.subtitle}` : ''}, ${label}`}
        style={({ pressed }) => (pressed ? styles.pressed : null)}
      >
        <Card surface="surface2" style={styles.topCard}>
          <Row
            leading={
              <Artwork
                uri={result.image_url}
                size={96}
                radius={isArtist ? radius.full : radius.lg}
                accessibilityLabel={result.title}
              />
            }
          >
            <View>
              <Text variant="title" numberOfLines={2}>
                {result.title}
              </Text>
              {result.subtitle != null && result.subtitle.length > 0 ? (
                <Text variant="body" tone="secondary" numberOfLines={1} style={styles.subtext}>
                  {result.subtitle}
                </Text>
              ) : null}
              <Text variant="body" tone="tertiary" style={styles.subtext}>
                {label}
              </Text>
            </View>
          </Row>
        </Card>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  topResultWrap: { marginBottom: spacing.lg },
  sectionHeader: { marginBottom: spacing.md, letterSpacing: 1 },
  pressed: { opacity: 0.85 },
  topCard: { paddingVertical: spacing.xl },
  subtext: { marginTop: spacing.xs },
});
