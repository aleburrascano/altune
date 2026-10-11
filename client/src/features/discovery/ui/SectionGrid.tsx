import { useState, type ReactElement } from 'react';
import { StyleSheet, View, type LayoutChangeEvent, type ViewStyle } from 'react-native';

import { Card, Text, radius, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { usePressableInteraction } from '@shared/ui/primitives/usePressableInteraction';

import { kindLabel, resultKey } from '@features/discovery/domain/resultDisplay';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { ResultTapHandler } from '@features/discovery/hooks/useResultTap';

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

function GridCardArt({ item }: { item: DiscoveryResult }): ReactElement {
  const artRadius = item.kind === 'artist' ? radius.full : radius.md;
  return (
    <Artwork uri={item.image_url} size={96} radius={artRadius} accessibilityLabel={item.title} />
  );
}

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

type GridContext = { columns: number; onPress: ResultTapHandler };

function gridCardStyle(theme: ReturnType<typeof useTheme>, highlighted: boolean) {
  return [styles.gridCard, highlighted ? { borderColor: theme.color.accent } : null];
}

function gridCardPressableProps(item: DiscoveryResult, position: number, grid: GridContext) {
  return {
    testID: `discover-grid-card-${item.kind}-${position}`,
    onPress: () => grid.onPress(item, position),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `${item.title}, ${kindLabel(item.kind)}`,
  };
}

const FULL_WIDTH_PERCENT = 100;

function gridCardSlotStyle(columns: number) {
  const slot = [
    { flexBasis: `${FULL_WIDTH_PERCENT / columns}%` as ViewStyle['flexBasis'] },
    styles.gridCardSlot,
  ];
  return () => slot;
}

type GridResultCardProps = { item: DiscoveryResult; position: number; grid: GridContext };

function GridResultCard({ item, position, grid }: GridResultCardProps): ReactElement {
  const theme = useTheme();
  const { hovered, focused, pressableProps } = usePressableInteraction(
    gridCardSlotStyle(grid.columns),
  );
  const testID = `discover-grid-card-body-${item.kind}-${position}`;
  return (
    <Tappable
      action="discover.grid.open"
      {...gridCardPressableProps(item, position, grid)}
      {...pressableProps}
    >
      <GridCardBody
        item={item}
        cardStyle={gridCardStyle(theme, hovered || focused)}
        testID={testID}
      />
    </Tappable>
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
