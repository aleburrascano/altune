import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Card, Row, Text, radius, spacing } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';

import { SectionLabel } from './SectionLabel';
import { kindLabel } from '../kindLabel';
import type { DiscoveryResult } from '@shared/api-client/discovery';

export function TopResultCard({
  result,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const isArtist = result.kind === 'artist';
  const label = kindLabel(result.kind);
  return (
    <View style={styles.topResultWrap}>
      <SectionLabel style={styles.sectionHeaderSpacing}>TOP RESULT</SectionLabel>
      <Pressable
        testID="discover-top-result"
        onPress={() => onPress(result, 0)}
        accessibilityRole="button"
        accessibilityLabel={`${result.title}${result.subtitle ? `, ${result.subtitle}` : ''}, ${label}`}
        style={({ pressed }) => (pressed ? styles.pressed : null)}
      >
        <Card style={styles.topCard}>
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
  sectionHeaderSpacing: { marginBottom: spacing.md },
  pressed: { opacity: 0.85 },
  topCard: { paddingVertical: spacing.xl },
  subtext: { marginTop: spacing.xs },
});
