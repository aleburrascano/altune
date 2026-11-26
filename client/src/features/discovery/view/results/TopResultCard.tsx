import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import {
  Card,
  Row,
  Text,
  radius,
  spacing,
  useTheme,
  Artwork,
  Tappable,
  usePressableInteraction,
  type PressableInteractionState,
  opacity,
} from '@shared/ui';
import { SectionLabel } from '@features/discovery/view/results/SectionLabel';
import { kindLabel } from '@features/discovery/core/resultDisplay';
import type { DiscoveryResult } from '@shared/catalog';
import type { ResultTapHandler } from '@features/discovery/view/results/useResultTap';

function pressedStyle({ pressed }: PressableInteractionState) {
  return pressed ? styles.pressed : null;
}

type TopResultTextProps = { item: DiscoveryResult; label: string };
type TopResultTitleProps = { title: string };
type TopResultSubtitleProps = { subtitle: string | null };
type TopResultKindProps = { label: string };

function TopResultTitle({ title }: TopResultTitleProps): ReactElement {
  return (
    <Text variant="title" numberOfLines={2}>
      {title}
    </Text>
  );
}

function TopResultSubtitle({ subtitle }: TopResultSubtitleProps): ReactElement | null {
  if (subtitle == null || subtitle.length === 0) return null;
  return (
    <Text variant="body" tone="secondary" numberOfLines={1} style={styles.subtext}>
      {subtitle}
    </Text>
  );
}

function TopResultKind({ label }: TopResultKindProps): ReactElement {
  return (
    <Text variant="body" tone="tertiary" style={styles.subtext}>
      {label}
    </Text>
  );
}

function TopResultText({ item, label }: TopResultTextProps): ReactElement {
  return (
    <View>
      <TopResultTitle title={item.title} />
      <TopResultSubtitle subtitle={item.subtitle} />
      <TopResultKind label={label} />
    </View>
  );
}

export function TopResultCard({
  result: item,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: ResultTapHandler;
}): ReactElement {
  const isArtist = item.kind === 'artist';
  const label = kindLabel(item.kind);
  const theme = useTheme();
  const { hovered, focused, pressableProps } = usePressableInteraction(pressedStyle);
  const highlighted = hovered || focused;
  return (
    <View style={styles.topResultWrap}>
      <SectionLabel style={styles.sectionHeaderSpacing}>TOP RESULT</SectionLabel>
      <Tappable
        action="discover.top_result.open"
        testID="discover-top-result"
        onPress={() => onPress(item, 0)}
        {...pressableProps}
        accessibilityRole="button"
        accessibilityLabel={`${item.title}${item.subtitle !== null && item.subtitle.length > 0 ? `, ${item.subtitle}` : ''}, ${label}`}
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
                uri={item.image_url}
                size={96}
                radius={isArtist ? radius.full : radius.lg}
                accessibilityLabel={item.title}
              />
            }
          >
            <TopResultText item={item} label={label} />
          </Row>
        </Card>
      </Tappable>
    </View>
  );
}

const styles = StyleSheet.create({
  topResultWrap: { marginBottom: spacing.lg },
  sectionHeaderSpacing: { marginBottom: spacing.md },
  pressed: { opacity: opacity.pressedSubtle },
  topCard: { paddingVertical: spacing.xl },
  subtext: { marginTop: spacing.xs },
});
