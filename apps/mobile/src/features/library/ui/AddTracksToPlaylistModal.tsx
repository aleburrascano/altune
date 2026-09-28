import { type ComponentProps, type ReactElement } from 'react';
import { ActivityIndicator, FlatList, Modal, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { Text, spacing, useTheme, type Theme } from '@shared/ui';
import { SearchBar } from '@shared/ui/primitives/SearchBar';

import type { Selection } from '../hooks/useSelection';
import { AddTracksHeader } from './AddTracksHeader';
import { AlreadyAddedRow } from './AlreadyAddedRow';
import { ConfirmButton } from './ConfirmButton';
import { LibraryRow } from './LibraryRow';
import { SelectAllButton } from './SelectAllButton';
import { useAddableTracks, type AddableTracksState, type SearchApi } from './useAddableTracks';

type AddTracksToPlaylistModalProps = {
  visible: boolean;
  playlistName: string;
  existingTrackIds: TrackId[];
  adding: boolean;
  onAdd: (trackIds: TrackId[]) => void;
  onClose: () => void;
};

type SectionProps = { props: AddTracksToPlaylistModalProps; state: AddableTracksState };

const SEARCH_BAR_CONFIG = {
  testID: 'add-tracks-search',
  placeholder: 'Search your library',
} as const;
const TRACK_LIST_CONFIG = {
  keyExtractor: (t: TrackResponse) => t.id,
  onEndReachedThreshold: 0.5,
  keyboardShouldPersistTaps: 'handled' as const,
};
const MODAL_CONFIG = { testID: 'add-tracks-modal', animationType: 'slide' as const };

type ModalUIProps = ComponentProps<typeof Modal>;

function modalProps(props: AddTracksToPlaylistModalProps, close: () => void): ModalUIProps {
  return { ...MODAL_CONFIG, visible: props.visible, onRequestClose: close };
}

function useScreenStyle() {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  return [styles.screen, { backgroundColor: theme.color.canvas, paddingTop: insets.top }];
}

function useFooterStyle() {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  return [
    styles.footer,
    { borderTopColor: theme.color.border, paddingBottom: insets.bottom + spacing.md },
  ];
}

function searchBarProps(search: SearchApi, theme: Theme): ComponentProps<typeof SearchBar> {
  return {
    ...SEARCH_BAR_CONFIG,
    value: search.inputValue,
    onChangeText: search.onChangeText,
    onSubmitEditing: search.onSubmit,
    onClear: search.onClear,
    theme,
  };
}

function AddTracksTopBar({ props, state }: SectionProps): ReactElement {
  return (
    <>
      <AddTracksHeader playlistName={props.playlistName} onBack={state.close} />
      <View style={styles.search}>
        <SearchBar {...searchBarProps(state.search, useTheme())} />
      </View>
    </>
  );
}

function LoadingSpinner(): ReactElement {
  return (
    <View style={styles.center}>
      <ActivityIndicator testID="add-tracks-loading" />
    </View>
  );
}

function EmptyResults({ hasQuery }: { hasQuery: boolean }): ReactElement {
  return (
    <View style={styles.center}>
      <Text variant="label" tone="secondary">
        {hasQuery ? 'No tracks match that search' : 'No tracks in your library yet'}
      </Text>
    </View>
  );
}

function listFooter(isFetchingNextPage: boolean): ReactElement | null {
  return isFetchingNextPage ? <ActivityIndicator style={styles.footerSpinner} /> : null;
}

type LibraryRowProps = ComponentProps<typeof LibraryRow>;

function libraryRowProps(item: TrackResponse, selection: Selection): LibraryRowProps {
  return {
    track: item,
    onPress: () => selection.toggle(item.id),
    onMore: () => selection.toggle(item.id),
    selectable: { selected: selection.has(item.id), onToggle: () => selection.toggle(item.id) },
  };
}

function renderTrackRow(existing: Set<TrackId>, selection: Selection) {
  const renderItem = ({ item }: { item: TrackResponse }): ReactElement => {
    if (existing.has(item.id)) return <AlreadyAddedRow title={item.title} />;
    return <LibraryRow {...libraryRowProps(item, selection)} />;
  };
  return renderItem;
}

function trackListProps(state: AddableTracksState) {
  return {
    ...TRACK_LIST_CONFIG,
    renderItem: renderTrackRow(state.existing, state.selection),
    onEndReached: state.query.onEndReached,
    ListFooterComponent: listFooter(state.query.isFetchingNextPage),
    ListEmptyComponent: <EmptyResults hasQuery={state.search.hasQuery} />,
  };
}

function TracksSection({ state }: { state: AddableTracksState }): ReactElement {
  if (state.query.isLoading) return <LoadingSpinner />;
  return <FlatList data={state.query.tracks} {...trackListProps(state)} />;
}

function selectAllHandler(state: AddableTracksState): () => void {
  return () => {
    if (state.allSelected) return state.selection.clear();
    return state.selection.selectAll(state.addable.map((t) => t.id));
  };
}

function selectAllProps(state: AddableTracksState): ComponentProps<typeof SelectAllButton> {
  return {
    allSelected: state.allSelected,
    disabled: state.addable.length === 0,
    onPress: selectAllHandler(state),
  };
}

type ConfirmUIProps = ComponentProps<typeof ConfirmButton>;

function confirmProps({ props, state }: SectionProps): ConfirmUIProps {
  return {
    playlistName: props.playlistName,
    count: state.selection.count,
    canConfirm: state.canConfirm,
    isEmpty: state.isEmpty,
    adding: props.adding,
    onPress: state.confirm,
  };
}

function ModalFooter({ props, state }: SectionProps): ReactElement {
  return (
    <View style={useFooterStyle()}>
      <SelectAllButton {...selectAllProps(state)} />
      <ConfirmButton {...confirmProps({ props, state })} />
    </View>
  );
}

function ModalContent({ props, state }: SectionProps): ReactElement {
  return (
    <View style={useScreenStyle()}>
      <AddTracksTopBar props={props} state={state} />
      <TracksSection state={state} />
      <ModalFooter props={props} state={state} />
    </View>
  );
}

export function AddTracksToPlaylistModal(props: AddTracksToPlaylistModalProps): ReactElement {
  const state = useAddableTracks(props);
  return (
    <Modal {...modalProps(props, state.close)}>
      <ModalContent props={props} state={state} />
    </Modal>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  search: { paddingHorizontal: spacing.lg, paddingBottom: spacing.md },
  center: { paddingTop: spacing['3xl'], alignItems: 'center' },
  footerSpinner: { paddingVertical: spacing.lg },
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.lg,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.md,
  },
});
