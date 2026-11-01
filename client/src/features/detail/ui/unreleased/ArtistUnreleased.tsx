import { useState, type ReactElement } from 'react';
import { Linking, StyleSheet, View } from 'react-native';

import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import { spacing } from '@shared/ui/theme';

import { useOpenUnreleased } from '@features/detail/hooks/useDetailNavigation';
import type { DetailRoute } from '@features/detail/domain/detailNavigation';
import type { UnreleasedWhere } from '@features/detail/domain/unreleasedView';
import { hasManyVersions } from '@features/detail/domain/unreleasedTrack';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import { useUnreleasedCard, useUnreleasedTracks } from '@features/detail/hooks/useUnreleasedTracks';
import { FilterChips, type ChipGroup } from '@features/detail/ui/DiscographySections';
import { DETAIL_GUTTER } from '@features/detail/ui/detailStyles';
import { AlbumCardsSkeleton } from '@features/detail/ui/DetailSkeleton';
import { Section, SectionError } from '@features/detail/ui/Section';
import { UnreleasedCard } from '@features/detail/ui/unreleased/UnreleasedCard';
import { UnreleasedVersionsSheet } from '@features/detail/ui/unreleased/UnreleasedVersionsSheet';

type UnreleasedArtistRowProps = { artist: DiscoveryResult; track: UnreleasedTrack };
type UnreleasedListProps = { artist: DiscoveryResult; tracks: UnreleasedTrack[] };
type ArtistUnreleasedProps = { artist: DiscoveryResult; detailRoute: DetailRoute };
type RailSectionProps = UnreleasedListProps & { detailRoute: DetailRoute };
type RetryProps = { onRetry: () => void };

function openGenius(track: UnreleasedTrack): void {
  if (track.genius_url === null) return;
  Linking.openURL(track.genius_url).catch((error: unknown) => {
    console.warn('[detail] could not open the Genius page', {
      geniusId: track.genius_id,
      error: error instanceof Error ? error.message : String(error),
    });
  });
}

function openLink(track: UnreleasedTrack): void {
  if (track.open_url === null) return;
  Linking.openURL(track.open_url).catch((error: unknown) => {
    console.warn('[detail] could not open the track link', {
      key: track.key,
      error: error instanceof Error ? error.message : String(error),
    });
  });
}

function useVersionsSheet(track: UnreleasedTrack) {
  const [open, setOpen] = useState(false);
  function onOpen() {
    setOpen(true);
  }
  if (!hasManyVersions(track)) return { onOpen, onLongPress: undefined, sheet: null };
  const props = { track, visible: open, onClose: () => setOpen(false) };
  return { onOpen, onLongPress: onOpen, sheet: <UnreleasedVersionsSheet {...props} /> };
}

function useRowCard({ artist, track }: UnreleasedArtistRowProps) {
  return useUnreleasedCard({
    track,
    artist,
    openGenius: () => openGenius(track),
    openLink: () => openLink(track),
  });
}

export function UnreleasedRow(props: UnreleasedArtistRowProps): ReactElement {
  const card = useRowCard(props);
  const versions = useVersionsSheet(props.track);
  return (
    <>
      <UnreleasedCard artist={props.artist} track={props.track} card={card} versions={versions} />
      {versions.sheet}
    </>
  );
}

function unreleasedKey(track: UnreleasedTrack): string {
  return track.key;
}

type UnreleasedItemInfo = { item: UnreleasedTrack };

function rowRenderer(artist: DiscoveryResult) {
  return function renderUnreleasedRow({ item }: UnreleasedItemInfo): ReactElement {
    return <UnreleasedRow artist={artist} track={item} />;
  };
}

const RAIL_LAYOUT = { testID: 'detail-unreleased-rail', keyExtractor: unreleasedKey };

function UnreleasedRail({ artist, tracks }: UnreleasedListProps): ReactElement {
  const renderItem = rowRenderer(artist);
  return <HorizontalRail {...RAIL_LAYOUT} {...railStyle()} data={tracks} renderItem={renderItem} />;
}

function railStyle() {
  return { style: styles.rail, contentContainerStyle: styles.railContent };
}

const SECTION_LABEL = 'Not on streaming';
const RELEASED: UnreleasedWhere = 'released';
const UNRELEASED: UnreleasedWhere = 'unreleased';

function trackGroups(tracks: UnreleasedTrack[]): ChipGroup[] {
  const released = tracks.filter((t) => t.off_streaming).length;
  const groups = [
    { type: RELEASED, label: 'Released', count: released },
    { type: UNRELEASED, label: 'Unreleased', count: tracks.length - released },
  ];
  return groups.filter((g) => g.count > 0);
}

function inGroup(tracks: UnreleasedTrack[], type: string): UnreleasedTrack[] {
  return tracks.filter((t) => t.off_streaming === (type === RELEASED));
}

function useTrackFilter(tracks: UnreleasedTrack[]) {
  const groups = trackGroups(tracks);
  const [picked, setPicked] = useState<string | null>(null);
  const activeType = groups.some((g) => g.type === picked) ? picked : groups[0]?.type;
  return { groups, activeType: activeType ?? UNRELEASED, onSelect: setPicked };
}

const CHIP_IDS = { action: 'detail.unreleased.select', testIDPrefix: 'detail-unreleased-group' };

const RAIL_CAP = 20;
const SEE_ALL_HIT_SLOP = 8;

type SeeAllProps = { total: number; onPress: () => void };
type SeeAllWhereProps = RailSectionProps & { where: UnreleasedWhere; total: number };

const SEE_ALL_STATIC = {
  action: 'detail.artist.unreleased.see_all',
  testID: 'detail-unreleased-see-all',
  hitSlop: SEE_ALL_HIT_SLOP,
  accessibilityRole: 'button' as const,
};

function seeAllPressable({ total, onPress }: SeeAllProps) {
  return { ...SEE_ALL_STATIC, onPress, accessibilityLabel: `See all (${total})` };
}

function SeeAllButton(props: SeeAllProps): ReactElement {
  return (
    <Tappable {...seeAllPressable(props)} style={styles.seeAll}>
      <Text variant="caption" tone="accent">{`See all (${props.total})`}</Text>
    </Tappable>
  );
}

function SeeAll(props: SeeAllWhereProps): ReactElement | null {
  const open = useOpenUnreleased(props.detailRoute);
  if (props.total <= RAIL_CAP) return null;
  return <SeeAllButton total={props.total} onPress={() => open(props.artist, props.where)} />;
}

function useRailView(tracks: UnreleasedTrack[]) {
  const filter = useTrackFilter(tracks);
  const where = filter.activeType === RELEASED ? RELEASED : UNRELEASED;
  return { filter, where, shown: inGroup(tracks, filter.activeType) };
}

function RailBody(props: RailSectionProps): ReactElement {
  const { filter, where, shown } = useRailView(props.tracks);
  return (
    <>
      <FilterChips {...filter} {...CHIP_IDS} />
      <SeeAll {...props} where={where} total={shown.length} />
      <UnreleasedRail artist={props.artist} tracks={shown.slice(0, RAIL_CAP)} />
    </>
  );
}

function UnreleasedSection(props: RailSectionProps): ReactElement {
  return (
    <View testID="detail-unreleased">
      <Section label={SECTION_LABEL}>
        <RailBody {...props} />
      </Section>
    </View>
  );
}

function UnreleasedLoading(): ReactElement {
  return (
    <View testID="detail-unreleased-loading">
      <Section label={SECTION_LABEL}>
        <AlbumCardsSkeleton />
      </Section>
    </View>
  );
}

const FAILED_MESSAGE = "Couldn't load unreleased tracks";

function UnreleasedFailed({ onRetry }: RetryProps): ReactElement {
  const error = { testIDPrefix: 'detail-unreleased', message: FAILED_MESSAGE, onRetry };
  return (
    <Section label={SECTION_LABEL}>
      <SectionError {...error} />
    </Section>
  );
}

export function ArtistUnreleased({
  artist,
  detailRoute,
}: ArtistUnreleasedProps): ReactElement | null {
  const { tracks, isLoading, isFailed, retry } = useUnreleasedTracks(artist);
  if (tracks.length > 0)
    return <UnreleasedSection artist={artist} tracks={tracks} detailRoute={detailRoute} />;
  if (isLoading) return <UnreleasedLoading />;
  return isFailed ? <UnreleasedFailed onRetry={retry} /> : null;
}

const styles = StyleSheet.create({
  rail: { marginHorizontal: -DETAIL_GUTTER },
  seeAll: { alignSelf: 'flex-end', marginBottom: spacing.sm },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
});
