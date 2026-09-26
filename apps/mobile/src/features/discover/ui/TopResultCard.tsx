import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Card, Row, Text, radius, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';

import { SectionLabel } from './SectionLabel';
import { kindLabel } from '../kindLabel';
import type { DiscoveryResult } from '@shared/api-client/discovery';

function buildHighlightProps(setHovered: (v: boolean) => void, setFocused: (v: boolean) => void) {
  return { onHoverIn: () => setHovered(true), onHoverOut: () => setHovered(false), onFocus: () => setFocused(true), onBlur: () => setFocused(false) };
}

function useHighlighted(): [boolean, ReturnType<typeof buildHighlightProps>] {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  return [hovered || focused, buildHighlightProps(setHovered, setFocused)];
}

export function TopResultCard({
  result,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const isArtist = result.kind === 'artist';
  const label = kindLabel(result.kind);
  const theme = useTheme();
  const [highlighted, highlightHandlers] = useHighlighted();
  return (
    <View style={styles.topResultWrap}>
      <SectionLabel style={styles.sectionHeaderSpacing}>TOP RESULT</SectionLabel>
      <Pressable
        testID="discover-top-result"
        onPress={() => onPress(result, 0)}
        {...highlightHandlers}
        accessibilityRole="button"
        accessibilityLabel={`${result.title}${result.subtitle ? `, ${result.subtitle}` : ''}, ${label}`}
        style={({ pressed }) => (pressed ? styles.pressed : null)}
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
