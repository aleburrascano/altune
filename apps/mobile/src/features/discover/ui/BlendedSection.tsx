import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View, useWindowDimensions, type ViewStyle } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Card, Text, radius, spacing, useLayoutMode, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';

import { DiscoverRow } from './DiscoverRow';
import { ResultsList, type ResultsCommonProps } from './ResultsList';
import { SectionLabel } from './SectionLabel';
import { TopResultCard } from './TopResultCard';
import { pressedStyle } from './pressedStyle';
import { kindLabel } from '../kindLabel';
import { resultKey } from '../resultKey';

import type { DiscoveryKind, DiscoveryResult, ResultSection } from '@shared/api-client/discovery';

function isGridKind(kind: DiscoveryKind): boolean {
  return kind === 'album' || kind === 'artist';
}

function gridColumnsFor(width: number): number {
  if (width >= 1600) return 6;
  if (width >= 1280) return 5;
  return 4;
}

/**
 * The client's own bound on rows per section, at twice the server's contract of 10
 * (discovery service `SectionCap`). Every row here mounts unvirtualized, so a server
 * that breaks that contract must truncate the section rather than drop frames.
 */
export const SECTION_ITEM_CAP = 20;

function buildHighlightProps(setHovered: (v: boolean) => void, setFocused: (v: boolean) => void) {
  return { onHoverIn: () => setHovered(true), onHoverOut: () => setHovered(false), onFocus: () => setFocused(true), onBlur: () => setFocused(false) };
}

function useHighlighted(): [boolean, ReturnType<typeof buildHighlightProps>] {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  return [hovered || focused, buildHighlightProps(setHovered, setFocused)];
}

function GridCardArt({ item }: { item: DiscoveryResult }): ReactElement {
  const isArtist = item.kind === 'artist';
  return <Artwork uri={item.image_url} size={96} radius={isArtist ? radius.full : radius.md} accessibilityLabel={item.title} />;
}

function GridCardBody({ item, cardStyle, testID }: { item: DiscoveryResult; cardStyle: (ViewStyle | null)[]; testID: string }): ReactElement {
  return (
    <Card testID={testID} style={cardStyle}>
      <GridCardArt item={item} />
      <Text variant="bodyStrong" numberOfLines={1} style={styles.gridTitle}>{item.title}</Text>
    </Card>
  );
}

function GridResultCard({ item, position, columns, onPress }: { item: DiscoveryResult; position: number; columns: number; onPress: (target: DiscoveryResult, at: number) => void }): ReactElement {
  const theme = useTheme();
  const highlight = useHighlighted();
  const cardStyle = [styles.gridCard, highlight[0] ? { borderColor: theme.color.accent } : null];
  return (
    <Pressable testID={`discover-grid-card-${item.kind}-${position}`} onPress={() => onPress(item, position)} accessibilityRole="button" accessibilityLabel={item.title} onHoverIn={highlight[1].onHoverIn} onHoverOut={highlight[1].onHoverOut} onFocus={highlight[1].onFocus} onBlur={highlight[1].onBlur} style={[{ flexBasis: `${100 / columns}%` }, styles.gridCardSlot]}>
      <GridCardBody item={item} cardStyle={cardStyle} testID={`discover-grid-card-body-${item.kind}-${position}`} />
    </Pressable>
  );
}

function SectionGrid({ items, columns, onPress }: { items: DiscoveryResult[]; columns: number; onPress: (target: DiscoveryResult, at: number) => void }): ReactElement {
  return (
    <View style={styles.grid} testID={`discover-grid-${items[0]?.kind ?? ''}`}>
      {items.map((item, index) => (
        <GridResultCard key={resultKey(item, index)} item={item} position={index} columns={columns} onPress={onPress} />
      ))}
    </View>
  );
}

function SectionRows({ items, onPress }: { items: DiscoveryResult[]; onPress: (target: DiscoveryResult, at: number) => void }): ReactElement {
  return (
    <>
      {items.map((item, index) => <DiscoverRow key={resultKey(item, index)} result={item} position={index} onPress={onPress} />)}
    </>
  );
}

function SeeAllLink({ kind, title, onSeeAll }: { kind: DiscoveryKind; title: string; onSeeAll: (kind: DiscoveryKind) => void }): ReactElement {
  const theme = useTheme();
  return (
    <Pressable testID={`discover-see-all-${kind}`} onPress={() => onSeeAll(kind)} accessibilityRole="button" accessibilityLabel={`See all ${title.toLowerCase()}`} hitSlop={8} style={({ pressed }) => [styles.seeAll, pressedStyle(pressed)]}>
      <Text variant="label" tone="accent">See all {title.toLowerCase()}</Text>
      <ChevronRight size={16} color={theme.color.accent} />
    </Pressable>
  );
}

function SectionBlock({ section, asGrid, columns, onSeeAll, onPress }: { section: ResultSection; asGrid: boolean; columns: number; onSeeAll: (kind: DiscoveryKind) => void; onPress: (target: DiscoveryResult, at: number) => void }): ReactElement {
  const title = kindLabel(section.kind, { plural: true }); const renderedItems = section.items.slice(0, SECTION_ITEM_CAP);
  const hasMoreThanRendered = section.has_more || section.items.length > renderedItems.length;
  return (
    <View style={styles.section}><SectionLabel style={styles.sectionHeaderSpacing}>{title.toUpperCase()}</SectionLabel>
      {asGrid ? <SectionGrid items={renderedItems} columns={columns} onPress={onPress} /> : <SectionRows items={renderedItems} onPress={onPress} />}
      {hasMoreThanRendered ? <SeeAllLink kind={section.kind} title={title} onSeeAll={onSeeAll} /> : null}
    </View>
  );
}

export function BlendedSection({ sections, topResult, onSeeAll, common }: { sections: ResultSection[]; topResult: DiscoveryResult | undefined; onSeeAll: (filter: DiscoveryKind) => void; common: ResultsCommonProps }): ReactElement {
  const isWide = useLayoutMode() === 'wide';
  const columns = gridColumnsFor(useWindowDimensions().width);
  const visible = sections.filter((section) => section.items.length > 0);
  const headerExtra = topResult !== undefined ? <TopResultCard result={topResult} onPress={common.onResultTap} /> : null;
  return (
    <ResultsList data={visible} keyExtractor={(section) => section.kind} pairFirstItemWithHeader={visible[0]?.kind === 'track'} headerExtra={headerExtra} common={common} renderItem={({ item: section }) => <SectionBlock section={section} asGrid={isWide && isGridKind(section.kind)} columns={columns} onSeeAll={onSeeAll} onPress={common.onResultTap} />} />
  );
}

const styles = StyleSheet.create({
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
