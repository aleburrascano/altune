import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View, type LayoutChangeEvent, type ViewStyle } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Card, Text, pressedStyle, radius, spacing, useTheme, useWideWebLayout } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';

import { DiscoverRow } from './DiscoverRow';
import { ResultsList, type ResultsCommonProps } from './ResultsList';
import { SectionLabel } from './SectionLabel';
import { TopResultCard } from './TopResultCard';
import { kindLabel } from '../kindLabel';
import { resultKey } from '../resultKey';

import type { DiscoveryKind, DiscoveryResult, ResultSection } from '@shared/api-client/discovery';

function isGridKind(kind: DiscoveryKind): boolean {
  return kind === 'album' || kind === 'artist';
}

function gridColumnsFor(contentWidth: number): number {
  if (contentWidth >= 1080) return 6;
  if (contentWidth >= 920) return 5;
  return 4;
}

const DEFAULT_GRID_WIDTH = 760;

function useGridColumns(): [number, (event: LayoutChangeEvent) => void] {
  const [gridWidth, setGridWidth] = useState(DEFAULT_GRID_WIDTH);
  return [
    gridColumnsFor(gridWidth),
    (event: LayoutChangeEvent) => setGridWidth(event.nativeEvent.layout.width),
  ];
}

export const SECTION_ITEM_CAP = 20;

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

type OnResultPress = (target: DiscoveryResult, at: number) => void;

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

function SectionGrid({ items, columns, onPress }: SectionGridProps): ReactElement {
  return (
    <View style={styles.grid} testID={`discover-grid-${items[0]?.kind ?? ''}`}>
      <GridResultCards items={items} columns={columns} onPress={onPress} />
    </View>
  );
}

type SectionRowsProps = { items: DiscoveryResult[]; onPress: OnResultPress };

function SectionRows({ items, onPress }: SectionRowsProps): ReactElement {
  return (
    <>
      {items.map((item, i) => (
        <DiscoverRow key={resultKey(item, i)} result={item} position={i} onPress={onPress} />
      ))}
    </>
  );
}

type SeeAllLinkProps = {
  kind: DiscoveryKind;
  title: string;
  onSeeAll: (kind: DiscoveryKind) => void;
};

function seeAllPressableProps({ kind, title, onSeeAll }: SeeAllLinkProps) {
  return {
    testID: `discover-see-all-${kind}`,
    onPress: () => onSeeAll(kind),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `See all ${title.toLowerCase()}`,
    hitSlop: 8,
    style: ({ pressed }: { pressed: boolean }) => [styles.seeAll, pressedStyle(pressed)],
  };
}

function SeeAllChevron(): ReactElement {
  const theme = useTheme();
  return <ChevronRight size={16} color={theme.color.accent} />;
}

function SeeAllLabel({ title }: { title: string }): ReactElement {
  return (
    <>
      <Text variant="label" tone="accent">
        See all {title.toLowerCase()}
      </Text>
      <SeeAllChevron />
    </>
  );
}

function SeeAllLink(props: SeeAllLinkProps): ReactElement {
  return (
    <Pressable {...seeAllPressableProps(props)}>
      <SeeAllLabel title={props.title} />
    </Pressable>
  );
}

type SectionActions = {
  columns: number;
  onSeeAll: (kind: DiscoveryKind) => void;
  onPress: OnResultPress;
};

type SectionBlockProps = { section: ResultSection; asGrid: boolean; actions: SectionActions };

type SectionItemsProps = {
  asGrid: boolean;
  renderedItems: DiscoveryResult[];
  actions: SectionActions;
};

function SectionItemsView({ asGrid, renderedItems, actions }: SectionItemsProps): ReactElement {
  return asGrid ? (
    <SectionGrid items={renderedItems} columns={actions.columns} onPress={actions.onPress} />
  ) : (
    <SectionRows items={renderedItems} onPress={actions.onPress} />
  );
}

function sectionBlockFacts(section: ResultSection) {
  const title = kindLabel(section.kind, { plural: true });
  const renderedItems = section.items.slice(0, SECTION_ITEM_CAP);
  const hasMoreThanRendered = section.has_more || section.items.length > renderedItems.length;
  return { title, renderedItems, hasMoreThanRendered };
}

function seeAllOrNull(
  hasMoreThanRendered: boolean,
  section: ResultSection,
  title: string,
  actions: SectionActions,
) {
  return hasMoreThanRendered ? (
    <SeeAllLink kind={section.kind} title={title} onSeeAll={actions.onSeeAll} />
  ) : null;
}

function SectionBlock({ section, asGrid, actions }: SectionBlockProps): ReactElement {
  const { title, renderedItems, hasMoreThanRendered } = sectionBlockFacts(section);
  return (
    <View style={styles.section}>
      <SectionLabel style={styles.sectionHeaderSpacing}>{title.toUpperCase()}</SectionLabel>
      <SectionItemsView asGrid={asGrid} renderedItems={renderedItems} actions={actions} />
      {seeAllOrNull(hasMoreThanRendered, section, title, actions)}
    </View>
  );
}

type BlendedSectionProps = {
  sections: ResultSection[];
  topResult: DiscoveryResult | undefined;
  onSeeAll: (filter: DiscoveryKind) => void;
  common: ResultsCommonProps;
};

function blendedHeaderExtra(topResult: DiscoveryResult | undefined, common: ResultsCommonProps) {
  return topResult !== undefined ? (
    <TopResultCard result={topResult} onPress={common.onResultTap} />
  ) : null;
}

type BlendedRenderContext = { isWide: boolean; actions: SectionActions };

type BlendedSectionItemProps = { section: ResultSection; ctx: BlendedRenderContext };

function BlendedSectionItem({ section, ctx }: BlendedSectionItemProps): ReactElement {
  const asGrid = ctx.isWide && isGridKind(section.kind);
  return <SectionBlock section={section} asGrid={asGrid} actions={ctx.actions} />;
}

function blendedSectionRenderItem(ctx: BlendedRenderContext) {
  function renderBlendedItem({ item: section }: { item: ResultSection }): ReactElement {
    return <BlendedSectionItem section={section} ctx={ctx} />;
  }
  return renderBlendedItem;
}

function useVisibleSections(sections: ResultSection[]): ResultSection[] {
  return sections.filter((section) => section.items.length > 0);
}

function useBlendedRenderContext(
  onSeeAll: (kind: DiscoveryKind) => void,
  common: ResultsCommonProps,
) {
  const isWide = useWideWebLayout();
  const [columns, onLayout] = useGridColumns();
  const actions = { columns, onSeeAll, onPress: common.onResultTap };
  return { ctx: { isWide, actions }, columns, onLayout };
}

type ResultsListArgs = {
  props: BlendedSectionProps;
  ctx: BlendedRenderContext;
  visible: ResultSection[];
};

function blendedResultsListProps({ props, ctx, visible }: ResultsListArgs) {
  const { topResult, common } = props;
  return {
    keyExtractor: (section: ResultSection) => section.kind,
    pairFirstItemWithHeader: visible[0]?.kind === 'track',
    headerExtra: blendedHeaderExtra(topResult, common),
    common,
    renderItem: blendedSectionRenderItem(ctx),
  };
}

export function BlendedSection(props: BlendedSectionProps): ReactElement {
  const { ctx, onLayout } = useBlendedRenderContext(props.onSeeAll, props.common);
  const visible = useVisibleSections(props.sections);
  const rest = blendedResultsListProps({ props, ctx, visible });
  return (
    <View style={styles.measure} onLayout={onLayout} testID="discover-blended-section">
      <ResultsList data={visible} {...rest} />
    </View>
  );
}

const styles = StyleSheet.create({
  measure: { flex: 1 },
  sectionHeaderSpacing: { marginBottom: spacing.sm, marginTop: spacing.sm },
  section: { marginBottom: spacing.xl },
  seeAll: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.md,
    alignSelf: 'flex-start',
    minHeight: 44,
  },
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  gridCardSlot: { padding: spacing.xs },
  gridCard: { alignItems: 'center', borderWidth: 2, borderColor: 'transparent' },
  gridTitle: { marginTop: spacing.sm, textAlign: 'center' },
});
