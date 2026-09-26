import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { albumExtras } from '../extras-accessors';
import { SECTION_CAP } from '../hooks/useDiscographyFilter';
import { albumYear } from './formatters';
import { DETAIL_GUTTER, DISCOGRAPHY_CARD_SIZE } from './layout';
import { sharedStyles } from './styles';

function railKey(album: DiscoveryResult, index: number): string {
  return `${album.title}-${album.sources[0]?.external_id ?? index}`;
}

type SeeAllProps = { typeKey: string; typeLabel: string; count: number; onPress: () => void };

function seeAllStyle(theme: ReturnType<typeof useTheme>) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.seeAll,
    { backgroundColor: theme.color.surface2 },
    pressed ? sharedStyles.pressed : null,
  ];
}

function seeAllPressableProps(props: SeeAllProps, theme: ReturnType<typeof useTheme>) {
  return {
    testID: `detail-see-all-${props.typeKey}`,
    onPress: props.onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: `See all ${props.count} ${props.typeLabel.toLowerCase()}`,
    style: seeAllStyle(theme),
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
  hasMore: boolean;
  typeKey: string;
  typeLabel: string;
  onAlbumPress: (album: DiscoveryResult) => void;
  onSeeAll: () => void;
};

function railFooter(props: AlbumRailProps): ReactElement | null {
  if (!props.hasMore) return null;
  return <SeeAllButton typeKey={props.typeKey} typeLabel={props.typeLabel} count={props.items.length} onPress={props.onSeeAll} />;
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

export function AlbumRail(props: AlbumRailProps): ReactElement {
  return <FlatList {...railStaticProps()} {...railDynamicProps(props)} data={props.items} />;
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

type AlbumCardProps = { album: DiscoveryResult; testID: string; typeLabel: string; onPress: () => void };

function albumCardLabel(props: AlbumCardProps, year: string | null, trackCount: number | null): string {
  const yearPart = year ? `, ${year}` : '';
  const trackPart = trackCount !== null ? `, ${trackCount} tracks` : '';
  return `${props.typeLabel}: ${props.album.title}${yearPart}${trackPart}`;
}

function albumCardPressableProps(props: AlbumCardProps, year: string | null, trackCount: number | null) {
  return {
    testID: props.testID,
    onPress: props.onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: albumCardLabel(props, year, trackCount),
    style: ({ pressed }: { pressed: boolean }) => [styles.card, pressed ? sharedStyles.pressed : null],
  };
}

function CardTitle({ title }: { title: string }): ReactElement {
  return (
    <Text variant="label" numberOfLines={2} style={styles.cardTitle}>
      {title}
    </Text>
  );
}

function CardBody(props: { album: DiscoveryResult; year: string | null; trackCount: number | null }): ReactElement {
  return (
    <>
      <Artwork uri={props.album.image_url} size={DISCOGRAPHY_CARD_SIZE} radius={radius.md} accessibilityLabel={props.album.title} />
      <CardTitle title={props.album.title} />
      <CardMeta year={props.year} trackCount={props.trackCount} />
    </>
  );
}

function AlbumCard(props: AlbumCardProps): ReactElement {
  const year = albumYear(props.album);
  const trackCount = albumExtras(props.album.extras).trackCount;
  return (
    <Pressable {...albumCardPressableProps(props, year, trackCount)}>
      <CardBody album={props.album} year={year} trackCount={trackCount} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  rail: { marginHorizontal: -DETAIL_GUTTER },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  card: { width: DISCOGRAPHY_CARD_SIZE },
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
});
