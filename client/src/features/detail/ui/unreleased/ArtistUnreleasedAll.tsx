import { Redirect, useLocalSearchParams } from 'expo-router';
import { useMemo, useState, type ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';

import { SortControl, type SortOption } from '@shared/ui/primitives/SortControl';
import { Text } from '@shared/ui/primitives/Text';
import { recordUserAction } from '@shared/telemetry/userTelemetry';
import { spacing } from '@shared/ui/theme';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';
import { readDetailHandoff } from '@shared/navigation/detailHandoff';

import {
  unreleasedView,
  type UnreleasedSort,
  type UnreleasedWhat,
  type UnreleasedWhere,
} from '@features/detail/domain/unreleasedView';
import { DetailHandoffProvider } from '@features/detail/hooks/useDetailHandoff';
import {
  useUnreleasedTracks,
  type UnreleasedTracksState,
} from '@features/detail/hooks/useUnreleasedTracks';
import { DETAIL_GUTTER } from '@features/detail/ui/detailStyles';
import { FilterChips, type ChipGroup } from '@features/detail/ui/DiscographySections';
import { AlbumCardsSkeleton } from '@features/detail/ui/DetailSkeleton';
import { SectionError } from '@features/detail/ui/Section';
import { UnreleasedRow } from '@features/detail/ui/unreleased/ArtistUnreleased';

type ArtistProps = { artist: DiscoveryResult };
type ListProps = ArtistProps & { tracks: UnreleasedTrack[] };
type RowInfo = { item: UnreleasedTrack };
type RetryProps = { onRetry: () => void };
type ControlsProps = { tracks: UnreleasedTrack[]; shownCount: number; choice: ViewChoice };
type BodyProps = {
  artist: DiscoveryResult;
  state: UnreleasedTracksState;
  choice: ViewChoice;
  shown: UnreleasedTrack[];
};
type ScreenParams = { handoff?: string; where?: string };

const INITIAL_ROWS_RENDERED = 12;
const PENDING_NOTE = 'Still finding more';
const EMPTY_NOTE = 'No playable tracks for this artist';
const FAILED_MESSAGE = "Couldn't load unreleased tracks";
const CHIP_IDS = { action: 'detail.unreleased.filter', isActionPerChip: true };
const SORT_IDS = { action: 'detail.unreleased.sort.open', testID: 'detail-unreleased-sort' };
const SORT_OPTIONS: SortOption<UnreleasedSort>[] = [
  { key: 'popular', label: 'Popular' },
  { key: 'best_quality', label: 'Best quality' },
  { key: 'most_versions', label: 'Most versions' },
  { key: 'newest', label: 'Newest' },
];
const SCREEN_TITLE = 'Not on streaming';
const WHERES: readonly UnreleasedWhere[] = ['all', 'released', 'unreleased'];

function trackKey(track: UnreleasedTrack): string {
  return track.key;
}

function isWhere(value: string | undefined): value is UnreleasedWhere {
  return WHERES.some((where) => where === value);
}

function railFirstChipWhere(tracks: UnreleasedTrack[]): UnreleasedWhere {
  return tracks.some((t) => t.off_streaming) ? 'released' : 'unreleased';
}

function rowRenderer(artist: DiscoveryResult) {
  return function renderRow({ item }: RowInfo): ReactElement {
    return (
      <View style={styles.row}>
        <UnreleasedRow artist={artist} track={item} />
      </View>
    );
  };
}

type ListState = { isPending: boolean };
type ListFrameProps = ListProps & ListState;

type NoteProps = { testID: string; text: string };

function Note({ testID, text }: NoteProps): ReactElement {
  return (
    <Text variant="body" tone="secondary" style={styles.note} testID={testID}>
      {text}
    </Text>
  );
}

function StillFinding(): ReactElement {
  return <Note testID="detail-unreleased-all-pending" text={PENDING_NOTE} />;
}

function EmptyNote(): ReactElement {
  return <Note testID="detail-unreleased-all-empty" text={EMPTY_NOTE} />;
}

function listExtras({ isPending }: ListState) {
  return {
    ListEmptyComponent: isPending ? null : <EmptyNote />,
    ListFooterComponent: isPending ? <StillFinding /> : null,
    contentContainerStyle: styles.content,
  };
}

function listProps(props: ListFrameProps) {
  return {
    testID: 'detail-unreleased-all-list',
    data: props.tracks,
    keyExtractor: trackKey,
    renderItem: rowRenderer(props.artist),
    initialNumToRender: INITIAL_ROWS_RENDERED,
    ...listExtras(props),
  };
}

function UnreleasedList(props: ListFrameProps): ReactElement {
  return <FlatList {...listProps(props)} />;
}

function isWhat(value: string): value is UnreleasedWhat {
  return value === 'all' || value === 'playable';
}

function useParamWhere(tracks: UnreleasedTrack[]): UnreleasedWhere {
  const params = useLocalSearchParams<ScreenParams>();
  return isWhere(params.where) ? params.where : railFirstChipWhere(tracks);
}

function useViewChoice(tracks: UnreleasedTrack[]) {
  const initialWhere = useParamWhere(tracks);
  const [sort, setSort] = useState<UnreleasedSort>('popular');
  const [pickedWhere, setWhere] = useState<UnreleasedWhere | null>(null);
  const [what, setWhat] = useState<UnreleasedWhat>('all');
  return { sort, setSort, where: pickedWhere ?? initialWhere, setWhere, what, setWhat };
}

type ViewChoice = ReturnType<typeof useViewChoice>;

function whereGroups(tracks: UnreleasedTrack[]): ChipGroup[] {
  const released = unreleasedView(tracks, 'popular', 'released', 'all').length;
  return [
    { type: 'released', label: 'Released', count: released },
    { type: 'unreleased', label: 'Unreleased', count: tracks.length - released },
  ];
}

function whatGroups(tracks: UnreleasedTrack[], where: UnreleasedWhere): ChipGroup[] {
  const inWhere = unreleasedView(tracks, 'popular', where, 'all');
  const playable = unreleasedView(tracks, 'popular', where, 'playable');
  return [
    { type: 'all', label: 'All', count: inWhere.length },
    { type: 'playable', label: 'Playable', count: playable.length },
  ];
}

function recordSortPick(key: UnreleasedSort): void {
  recordUserAction({ action: `detail.unreleased.sort.${key}`, outcome: 'tapped' });
}

function sortPicker(choice: ViewChoice) {
  return (key: UnreleasedSort): void => {
    recordSortPick(key);
    choice.setSort(key);
  };
}

function sortProps({ shownCount, choice }: ControlsProps) {
  return {
    count: shownCount,
    noun: 'track',
    sortKey: choice.sort,
    options: SORT_OPTIONS,
    onSortChange: sortPicker(choice),
    ...SORT_IDS,
  };
}

function ViewControls(props: ControlsProps): ReactElement {
  const { tracks, choice } = props;
  return (
    <View style={styles.controls}>
      <SortControl {...sortProps(props)} />
      <FilterChips {...whereChips(tracks, choice)} />
      <FilterChips {...whatChips(tracks, choice)} />
    </View>
  );
}

function whereChips(tracks: UnreleasedTrack[], choice: ViewChoice) {
  return {
    ...CHIP_IDS,
    groups: whereGroups(tracks),
    activeType: choice.where,
    onSelect: (type: string) => isWhere(type) && choice.setWhere(type),
    testIDPrefix: 'detail-unreleased-all-where',
  };
}

function whatChips(tracks: UnreleasedTrack[], choice: ViewChoice) {
  return {
    ...CHIP_IDS,
    groups: whatGroups(tracks, choice.where),
    activeType: choice.what,
    onSelect: (type: string) => isWhat(type) && choice.setWhat(type),
    testIDPrefix: 'detail-unreleased-all-what',
  };
}

function ScreenTitle(): ReactElement {
  return (
    <Text variant="title" style={styles.title}>
      {SCREEN_TITLE}
    </Text>
  );
}

function UnreleasedFailed({ onRetry }: RetryProps): ReactElement {
  const error = { testIDPrefix: 'detail-unreleased-all', message: FAILED_MESSAGE, onRetry };
  return <SectionError {...error} />;
}

function UnreleasedAllBody({ artist }: ArtistProps): ReactElement {
  const state = useUnreleasedTracks(artist);
  const choice = useViewChoice(state.tracks);
  const { tracks } = state;
  const { sort, where, what } = choice;
  const shown = useMemo(
    () => unreleasedView(tracks, sort, where, what),
    [tracks, sort, where, what],
  );
  return (
    <View style={styles.screen} testID="detail-unreleased-all">
      <ScreenTitle />
      <BodyStates {...{ artist, state, choice, shown }} />
    </View>
  );
}

function isEmptyAnd(state: UnreleasedTracksState, flag: boolean): boolean {
  return state.tracks.length === 0 && flag;
}

function LoadedBody({ artist, state, choice, shown }: BodyProps): ReactElement {
  const controls = { tracks: state.tracks, shownCount: shown.length, choice };
  return (
    <>
      <ViewControls {...controls} />
      <UnreleasedList artist={artist} tracks={shown} isPending={state.isPending} />
    </>
  );
}

function BodyStates(props: BodyProps): ReactElement {
  const { state } = props;
  if (isEmptyAnd(state, state.isLoading)) return <AlbumCardsSkeleton />;
  if (isEmptyAnd(state, state.isFailed)) return <UnreleasedFailed onRetry={state.retry} />;
  return <LoadedBody {...props} />;
}

export function UnreleasedAllScreen(): ReactElement {
  const params = useLocalSearchParams<ScreenParams>();
  const handoff = readDetailHandoff(params.handoff);
  if (handoff === null) return <Redirect href="/discover" />;
  return (
    <DetailHandoffProvider value={handoff}>
      <UnreleasedAllBody artist={handoff.result} />
    </DetailHandoffProvider>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  title: { paddingHorizontal: DETAIL_GUTTER, paddingVertical: spacing.md },
  content: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  controls: { paddingHorizontal: DETAIL_GUTTER },
  note: { paddingVertical: spacing.md, textAlign: 'center' },
  row: { alignItems: 'flex-start' },
});
