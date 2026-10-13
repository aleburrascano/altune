import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Text, pressedStyle, spacing, useTheme, useWideWebLayout } from '@shared/ui';
import { Tappable } from '@shared/ui/primitives/Tappable';

import { DiscoverRow } from '@features/discovery/ui/DiscoverRow';
import { ResultsList, type ResultsCommonProps } from '@features/discovery/ui/ResultsList';
import { SectionLabel } from '@features/discovery/ui/SectionLabel';
import { SectionGrid, useGridColumns } from '@features/discovery/ui/SectionGrid';
import { TopResultCard } from '@features/discovery/ui/TopResultCard';
import { kindLabel, resultKey } from '@features/discovery/domain/resultDisplay';

import type { ResultTapHandler } from '@features/discovery/hooks/useResultTap';
import type { DiscoveryKind, DiscoveryResult, ResultSection } from '@shared/apiClient/discovery';
import { touchTarget } from '@shared/ui/theme';

type SeeAllLinkProps = {
  kind: DiscoveryKind;
  title: string;
  onSeeAll: (kind: DiscoveryKind) => void;
};

type PressState = { pressed: boolean };

function seeAllPressableProps({ kind, title, onSeeAll }: SeeAllLinkProps) {
  return {
    testID: `discover-see-all-${kind}`,
    onPress: () => onSeeAll(kind),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `See all ${title.toLowerCase()}`,
    hitSlop: 8,
    style: ({ pressed }: PressState) => [seeAllStyles.seeAll, pressedStyle(pressed)],
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
    <Tappable action="discover.section.see_all" {...seeAllPressableProps(props)}>
      <SeeAllLabel title={props.title} />
    </Tappable>
  );
}

const seeAllStyles = StyleSheet.create({
  seeAll: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.md,
    alignSelf: 'flex-start',
    minHeight: touchTarget,
  },
});

function isGridKind(kind: DiscoveryKind): boolean {
  return kind === 'album' || kind === 'artist';
}

const SECTION_ITEM_CAP = 20;

type SectionRowsProps = { items: DiscoveryResult[]; onPress: ResultTapHandler };

function SectionRows({ items, onPress }: SectionRowsProps): ReactElement {
  return (
    <>
      {items.map((item, i) => (
        <DiscoverRow key={resultKey(item, i)} result={item} position={i} onPress={onPress} />
      ))}
    </>
  );
}

type SectionActions = {
  columns: number;
  onSeeAll: (kind: DiscoveryKind) => void;
  onPress: ResultTapHandler;
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

function visibleSections(sections: ResultSection[]): ResultSection[] {
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
  const visible = visibleSections(props.sections);
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
});
