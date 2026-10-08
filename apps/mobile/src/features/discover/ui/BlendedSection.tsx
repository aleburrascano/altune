import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { spacing, useWideWebLayout } from '@shared/ui';

import { DiscoverRow } from '@features/discover/ui/DiscoverRow';
import { ResultsList, type ResultsCommonProps } from '@features/discover/ui/ResultsList';
import { SectionLabel } from '@features/discover/ui/SectionLabel';
import { SectionGrid, useGridColumns, type OnResultPress } from '@features/discover/ui/SectionGrid';
import { SeeAllLink } from '@features/discover/ui/SeeAllLink';
import { TopResultCard } from '@features/discover/ui/TopResultCard';
import { kindLabel } from '@features/discover/kindLabel';
import { resultKey } from '@features/discover/resultKey';

import type { DiscoveryKind, DiscoveryResult, ResultSection } from '@shared/api-client/discovery';

function isGridKind(kind: DiscoveryKind): boolean {
  return kind === 'album' || kind === 'artist';
}

export const SECTION_ITEM_CAP = 20;

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
