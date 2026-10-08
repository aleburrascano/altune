import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View, type LayoutChangeEvent, type ViewStyle } from 'react-native';

import { Card, Text, radius, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';

import { kindLabel } from '@features/discover/kindLabel';
import { resultKey } from '@features/discover/resultKey';

import type { DiscoveryResult } from '@shared/api-client/discovery';

function gridColumnsFor(contentWidth: number): number {
  if (contentWidth >= 1080) return 6;
  if (contentWidth >= 920) return 5;
  return 4;
}

const DEFAULT_GRID_WIDTH = 760;

export function useGridColumns(): [number, (event: LayoutChangeEvent) => void] {
  const [gridWidth, setGridWidth] = useState(DEFAULT_GRID_WIDTH);
  return [
    gridColumnsFor(gridWidth),
    (event: LayoutChangeEvent) => setGridWidth(event.nativeEvent.layout.width),
  ];
}

function buildHighlightProps(setHovered: (v: boolean) => void, setFocused: (v: boolean) => void) {
  return {
    onHoverIn: () => setHovered(true),
    onHoverOut: () => setHovered(false),
    onFocus: () => setFocused(true),
    onBlur: () => setFocused(false),
  };
}

function useHighlighted(): [boolean, ReturnType<typeof buildHighlightProps>] {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  return [hovered || focused, buildHighlightProps(setHovered, setFocused)];
}

function GridCardArt({ item }: { item: DiscoveryResult }): ReactElement {
  const isArtist = item.kind === 'artist';
  return (
    <Artwork
      uri={item.image_url}
      size={96}
      radius={isArtist ? radius.full : radius.md}
      accessibilityLabel={item.title}
    />
  );
}

export type OnResultPress = (target: DiscoveryResult, at: number) => void;

type GridCardBodyProps = { item: DiscoveryResult; cardStyle: (ViewStyle | null)[]; testID: string };

function GridCardBody({ item, cardStyle, testID }: GridCardBodyProps): ReactElement {
  return (
    <Card testID={testID} style={cardStyle}>
      <GridCardArt item={item} />
      <Text variant="bodyStrong" numberOfLines={1} style={styles.gridTitle}>
        {item.title}
      </Text>
    </Card>
  );
}

type GridContext = { columns: number; onPress: OnResultPress };

function gridCardStyle(theme: ReturnType<typeof useTheme>, highlighted: boolean) {
  return [styles.gridCard, highlighted ? { borderColor: theme.color.accent } : null];
}

function gridCardPressableProps(item: DiscoveryResult, position: number, grid: GridContext) {
  return {
    testID: `discover-grid-card-${item.kind}-${position}`,
    onPress: () => grid.onPress(item, position),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `${item.title}, ${kindLabel(item.kind)}`,
    style: [{ flexBasis: `${100 / grid.columns}%` as ViewStyle['flexBasis'] }, styles.gridCardSlot],
  };
}

type GridResultCardProps = { item: DiscoveryResult; position: number; grid: GridContext };

function GridResultCard({ item, position, grid }: GridResultCardProps): ReactElement {
  const theme = useTheme();
  const [highlighted, highlightProps] = useHighlighted();
  const testID = `discover-grid-card-body-${item.kind}-${position}`;
  return (
    <Pressable {...gridCardPressableProps(item, position, grid)} {...highlightProps}>
      <GridCardBody item={item} cardStyle={gridCardStyle(theme, highlighted)} testID={testID} />
    </Pressable>
  );
}

type SectionGridProps = { items: DiscoveryResult[] } & GridContext;

function GridResultCards({ items, columns, onPress }: SectionGridProps): ReactElement[] {
  const grid = { columns, onPress };
  return items.map((item, position) => (
    <GridResultCard key={resultKey(item, position)} item={item} position={position} grid={grid} />
  ));
}

export function SectionGrid({ items, columns, onPress }: SectionGridProps): ReactElement {
  return (
    <View style={styles.grid} testID={`discover-grid-${items[0]?.kind ?? ''}`}>
      <GridResultCards items={items} columns={columns} onPress={onPress} />
    </View>
  );
}

const styles = StyleSheet.create({
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  gridCardSlot: { padding: spacing.xs },
  gridCard: { alignItems: 'center', borderWidth: 2, borderColor: 'transparent' },
  gridTitle: { marginTop: spacing.sm, textAlign: 'center' },
});
