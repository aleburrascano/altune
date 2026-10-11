import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Card, Row, Text, radius, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import {
  usePressableInteraction,
  type PressableInteractionState,
} from '@shared/ui/primitives/usePressableInteraction';

import { SectionLabel } from '@features/discovery/ui/SectionLabel';
import { kindLabel } from '@features/discovery/domain/resultDisplay';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { ResultTapHandler } from '@features/discovery/hooks/useResultTap';

function pressedStyle({ pressed }: PressableInteractionState) {
  return pressed ? styles.pressed : null;
}

export function TopResultCard({
  result,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: ResultTapHandler;
}): ReactElement {
  const isArtist = result.kind === 'artist';
  const label = kindLabel(result.kind);
  const theme = useTheme();
  const { hovered, focused, pressableProps } = usePressableInteraction(pressedStyle);
  const highlighted = hovered || focused;
  return (
    <View style={styles.topResultWrap}>
      <SectionLabel style={styles.sectionHeaderSpacing}>TOP RESULT</SectionLabel>
      <Tappable
        action="discover.top_result.open"
        testID="discover-top-result"
        onPress={() => onPress(result, 0)}
        {...pressableProps}
        accessibilityRole="button"
        accessibilityLabel={`${result.title}${result.subtitle ? `, ${result.subtitle}` : ''}, ${label}`}
      >
        <Card
          testID="discover-top-result-card"
          style={[
            styles.topCard,
            { borderWidth: 2, borderColor: highlighted ? theme.color.accent : 'transparent' },
          ]}
        >
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
      </Tappable>
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
