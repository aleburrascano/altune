import { useState, type ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import type { LayoutChangeEvent, PressableStateCallbackType } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { useWideWebLayout } from '@shared/ui/layout';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { albumExtras } from '../extras-accessors';
import { SECTION_CAP } from '../hooks/useDiscographyFilter';
import { albumYear } from './formatters';
import { DETAIL_GUTTER, DISCOGRAPHY_CARD_SIZE, gridCellWidthFor, gridColumnsFor } from './layout';
import { sharedStyles } from './styles';

type PressableWebState = PressableStateCallbackType & { hovered?: boolean; focused?: boolean };

function railKey(album: DiscoveryResult, index: number): string {
  return `${album.title}-${album.sources[0]?.external_id ?? index}`;
}

type SeeAllProps = { typeKey: string; typeLabel: string; total: number; onPress: () => void; size: number };

function seeAllStyle(theme: ReturnType<typeof useTheme>, size: number) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.seeAll,
    { width: size, height: size },
    { backgroundColor: theme.color.surface2 },
    pressed ? sharedStyles.pressed : null,
  ];
}

function seeAllPressableProps(props: SeeAllProps, theme: ReturnType<typeof useTheme>) {
  return {
    testID: `detail-see-all-${props.typeKey}`,
    onPress: props.onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: `See all ${props.total} ${props.typeLabel.toLowerCase()}`,
    style: seeAllStyle(theme, props.size),
  };
}

function SeeAllContent({ theme }: { theme: ReturnType<typeof useTheme> }): ReactElement {
  return (
    <>
      <ChevronRight size={20} color={theme.color.accent} />
      <Text variant="label" tone="accent" style={styles.seeAllText}>
        See all
      </Text>
    </>
  );
}

function SeeAllButton(props: SeeAllProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...seeAllPressableProps(props, theme)}>
      <SeeAllContent theme={theme} />
    </Pressable>
  );
}

type AlbumRailProps = {
  items: DiscoveryResult[];
  total: number;
  hasMore: boolean;
  typeKey: string;
  typeLabel: string;
  onAlbumPress: (album: DiscoveryResult) => void;
  onSeeAll: () => void;
};

function railFooter(props: AlbumRailProps, size: number = DISCOGRAPHY_CARD_SIZE): ReactElement | null {
  if (!props.hasMore) return null;
  return (
    <SeeAllButton typeKey={props.typeKey} typeLabel={props.typeLabel} total={props.total} onPress={props.onSeeAll} size={size} />
  );
}

type RailCardProps = {
  item: DiscoveryResult;
  index: number;
  typeKey: string;
  typeLabel: string;
  onAlbumPress: (album: DiscoveryResult) => void;
};

function RailCard(props: RailCardProps): ReactElement {
  const onPress = () => props.onAlbumPress(props.item);
  return <AlbumCard album={props.item} testID={`detail-${props.typeKey}-${props.index}`} typeLabel={props.typeLabel} onPress={onPress} />;
}

function railStyleProps() {
  return { style: styles.rail, contentContainerStyle: styles.railContent };
}

function railStaticProps() {
  return {
    testID: 'detail-discography-rail',
    horizontal: true as const,
    showsHorizontalScrollIndicator: false,
    initialNumToRender: SECTION_CAP,
    keyExtractor: railKey,
    ...railStyleProps(),
  };
}

function railDynamicProps(props: AlbumRailProps) {
  return {
    renderItem: ({ item, index }: { item: DiscoveryResult; index: number }) => (
      <RailCard item={item} index={index} typeKey={props.typeKey} typeLabel={props.typeLabel} onAlbumPress={props.onAlbumPress} />
    ),
    ListFooterComponent: railFooter(props),
  };
}

function AlbumRailList(props: AlbumRailProps): ReactElement {
  return <FlatList {...railStaticProps()} {...railDynamicProps(props)} data={props.items} />;
}

type GridCardProps = RailCardProps & { cardWidth: number };

function gridCardProps(props: GridCardProps) {
  return {
    album: props.item,
    testID: `detail-${props.typeKey}-${props.index}`,
    typeLabel: props.typeLabel,
    onPress: () => props.onAlbumPress(props.item),
    cardWidth: props.cardWidth,
    isGrid: true,
  };
}

function GridCard(props: GridCardProps): ReactElement {
  return <AlbumCard {...gridCardProps(props)} />;
}

function gridStaticProps(columns: number) {
  return {
    testID: 'detail-discography-grid',
    numColumns: columns,
    initialNumToRender: SECTION_CAP,
    keyExtractor: railKey,
    columnWrapperStyle: styles.gridRow,
    contentContainerStyle: styles.gridContent,
  };
}

function gridDynamicProps(props: AlbumRailProps, cardWidth: number) {
  return {
    renderItem: ({ item, index }: { item: DiscoveryResult; index: number }) => (
      <GridCard item={item} index={index} typeKey={props.typeKey} typeLabel={props.typeLabel} onAlbumPress={props.onAlbumPress} cardWidth={cardWidth} />
    ),
    ListFooterComponent: railFooter(props, cardWidth),
  };
}

function onGridLayout(setWidth: (width: number) => void) {
  return (event: LayoutChangeEvent) => setWidth(event.nativeEvent.layout.width);
}

function gridGeometry(width: number): { columns: number; cardWidth: number } {
  const columns = gridColumnsFor(width);
  const cardWidth = width > 0 ? gridCellWidthFor(width, columns) : DISCOGRAPHY_CARD_SIZE;
  return { columns, cardWidth };
}

function AlbumGrid(props: AlbumRailProps): ReactElement {
  const [width, setWidth] = useState(0);
  const { columns, cardWidth } = gridGeometry(width);
  return (
    <View testID="detail-discography-grid-measure" onLayout={onGridLayout(setWidth)}>
      <FlatList key={`detail-discography-grid-${columns}`} {...gridStaticProps(columns)} {...gridDynamicProps(props, cardWidth)} data={props.items} />
    </View>
  );
}

export function AlbumRail(props: AlbumRailProps): ReactElement {
  const wide = useWideWebLayout();
  if (wide) {
    return <AlbumGrid {...props} />;
  }
  return <AlbumRailList {...props} />;
}

function YearCaption({ year }: { year: string | null }): ReactElement | null {
  if (year === null) return null;
  return (
    <Text variant="caption" tone="tertiary">
      {year}
    </Text>
  );
}

function TrackCountCaption({ trackCount }: { trackCount: number | null }): ReactElement | null {
  if (trackCount === null) return null;
  return (
    <Text variant="caption" tone="tertiary">
      {trackCount} tracks
    </Text>
  );
}

function CardMeta({ year, trackCount }: { year: string | null; trackCount: number | null }): ReactElement {
  return (
    <>
      <YearCaption year={year} />
      <TrackCountCaption trackCount={trackCount} />
    </>
  );
}

type AlbumCardProps = {
  album: DiscoveryResult;
  testID: string;
  typeLabel: string;
  onPress: () => void;
  cardWidth?: number;
  isGrid?: boolean;
};

function albumCardLabel(props: AlbumCardProps, year: string | null, trackCount: number | null): string {
  const yearPart = year ? `, ${year}` : '';
  const trackPart = trackCount !== null ? `, ${trackCount} tracks` : '';
  return `${props.typeLabel}: ${props.album.title}${yearPart}${trackPart}`;
}

function albumCardStyle(theme: ReturnType<typeof useTheme>, cardWidth: number, isGrid: boolean) {
  return ({ pressed, hovered, focused }: PressableWebState) => [
    styles.card,
    { width: cardWidth },
    isGrid ? styles.gridCard : null,
    isGrid && hovered ? { backgroundColor: theme.color.surface2 } : null,
    isGrid ? { borderColor: focused ? theme.color.accent : 'transparent' } : null,
    pressed ? sharedStyles.pressed : null,
  ];
}

type AlbumCardPressableArgs = {
  props: AlbumCardProps;
  year: string | null;
  trackCount: number | null;
  theme: ReturnType<typeof useTheme>;
};

function albumCardPressableProps({ props, year, trackCount, theme }: AlbumCardPressableArgs) {
  return {
    testID: props.testID,
    onPress: props.onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: albumCardLabel(props, year, trackCount),
    style: albumCardStyle(theme, props.cardWidth ?? DISCOGRAPHY_CARD_SIZE, props.isGrid ?? false),
  };
}

function CardTitle({ title }: { title: string }): ReactElement {
  return (
    <Text variant="label" numberOfLines={2} style={styles.cardTitle}>
      {title}
    </Text>
  );
}

function CardBody(props: { album: DiscoveryResult; year: string | null; trackCount: number | null; cardWidth: number }): ReactElement {
  return (
    <>
      <Artwork uri={props.album.image_url} size={props.cardWidth} radius={radius.md} accessibilityLabel={props.album.title} />
      <CardTitle title={props.album.title} />
      <CardMeta year={props.year} trackCount={props.trackCount} />
    </>
  );
}

function AlbumCard(props: AlbumCardProps): ReactElement {
  const theme = useTheme();
  const year = albumYear(props.album);
  const trackCount = albumExtras(props.album.extras).trackCount;
  const cardWidth = props.cardWidth ?? DISCOGRAPHY_CARD_SIZE;
  return (
    <Pressable {...albumCardPressableProps({ props, year, trackCount, theme })}>
      <CardBody album={props.album} year={year} trackCount={trackCount} cardWidth={cardWidth} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  rail: { marginHorizontal: -DETAIL_GUTTER },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  card: {
    width: DISCOGRAPHY_CARD_SIZE,
  },
  gridCard: {
    borderWidth: 2,
    borderRadius: radius.md,
  },
  cardTitle: { marginTop: spacing.xs },
  seeAll: {
    width: DISCOGRAPHY_CARD_SIZE,
    height: DISCOGRAPHY_CARD_SIZE,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.md,
    gap: spacing.xs,
  },
  seeAllText: { textAlign: 'center' },
  gridRow: { gap: spacing.md, marginBottom: spacing.md },
  gridContent: { gap: spacing.md },
});
