/**
 * DiscoverRow — the signature art-forward result card.
 *
 * Slice 46 + ADR-0008. testID = `discover-row-<kind>-<position>` (preserved).
 * Art leads; title/subtitle stack; kind chip; trailing confidence dot + source
 * count. High-confidence multi-source results get the subtle verified glow.
 */

import type { ReactElement } from 'react';
import { Pressable, View } from 'react-native';

import { Artwork, Card, Chip, ConfidenceDot, Row, Text, spacing } from '@shared/ui';

import type { DiscoveryResult } from '../../../shared/api-client/discovery';

export type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: (result: DiscoveryResult, position: number) => void;
};

export function DiscoverRow({ result, position, onPress }: DiscoverRowProps): ReactElement {
  const testId = `discover-row-${result.kind}-${position}`;
  const verified = result.confidence === 'high' && result.sources.length > 1;

  return (
    <Pressable
      testID={testId}
      onPress={() => onPress(result, position)}
      style={({ pressed }) => (pressed ? { opacity: 0.85 } : null)}
    >
      <Card active={verified} style={{ marginBottom: spacing.sm }}>
        <Row
          leading={<Artwork uri={result.image_url} size={52} accessibilityLabel={result.title} />}
          trailing={
            <View style={{ alignItems: 'flex-end', gap: 4 }}>
              <ConfidenceDot level={result.confidence} />
              {result.sources.length > 1 ? (
                <Text variant="caption" tone="tertiary">
                  {result.sources.length}
                </Text>
              ) : null}
            </View>
          }
        >
          <Text variant="bodyStrong" numberOfLines={1}>
            {result.title}
          </Text>
          {result.subtitle !== null ? (
            <Text variant="label" tone="secondary" numberOfLines={1} style={{ marginTop: 2 }}>
              {result.subtitle}
            </Text>
          ) : null}
          <View style={{ marginTop: 6, alignSelf: 'flex-start' }}>
            <Chip label={result.kind.toUpperCase()} />
          </View>
        </Row>
      </Card>
    </Pressable>
  );
}
