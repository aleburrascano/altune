import { useMemo, type ReactElement } from 'react';
import { ActivityIndicator, FlatList, Modal, Pressable, StyleSheet, View } from 'react-native';
import { ChevronLeft } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { TrackResponse } from '@shared/api-client/types';
import { Text, minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { SearchBar } from '@shared/ui/primitives/SearchBar';

import { useLibrarySearch } from '../hooks/useLibrarySearch';
import { useLibraryTracks } from '../hooks/useLibraryHome';
import { useSelection } from '../useSelection';
import { LibraryRow } from './LibraryRow';

type AddTracksToPlaylistModalProps = {
  visible: boolean;
  playlistName: string;
  existingTrackIds: string[];
  adding: boolean;
  onAdd: (trackIds: string[]) => void;
  onClose: () => void;
};

export function AddTracksToPlaylistModal({
  visible,
  playlistName,
  existingTrackIds,
  adding,
  onAdd,
  onClose,
}: AddTracksToPlaylistModalProps): ReactElement {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const search = useLibrarySearch();
  const selection = useSelection();
  const { tracks, isLoading, isFetchingNextPage, onEndReached } = useLibraryTracks(
    search.query,
    'recent',
    visible,
  );

  const existing = useMemo(() => new Set(existingTrackIds), [existingTrackIds]);
  const addable = tracks.filter((t) => !existing.has(t.id));
  const allSelected = addable.length > 0 && selection.count === addable.length;

  const close = (): void => {
    selection.clear();
    search.onClear();
    onClose();
  };

  const confirm = (): void => {
    if (selection.count === 0) return;
    onAdd(selection.ids);
  };

  const renderItem = ({ item }: { item: TrackResponse }): ReactElement => {
    if (existing.has(item.id)) {
      return (
        <View style={styles.alreadyRow}>
          <Text variant="body" numberOfLines={1} tone="tertiary" style={styles.alreadyTitle}>
            {item.title}
          </Text>
          <Text variant="caption" tone="tertiary">
            Added
          </Text>
        </View>
      );
    }
    return (
      <LibraryRow
        track={item}
        onPress={() => selection.toggle(item.id)}
        onMore={() => selection.toggle(item.id)}
        selectable={{ selected: selection.has(item.id), onToggle: () => selection.toggle(item.id) }}
      />
    );
  };

  return (
    <Modal
      testID="add-tracks-modal"
      visible={visible}
      animationType="slide"
      onRequestClose={close}
    >
      <View
        style={[styles.screen, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}
      >
        <View style={styles.header}>
          <IconButton icon={ChevronLeft} size={24} onPress={close} accessibilityLabel="Back" />
          <Text variant="title" numberOfLines={1} style={styles.headerTitle}>
            Add to {playlistName}
          </Text>
        </View>

        <View style={styles.search}>
          <SearchBar
            testID="add-tracks-search"
            value={search.inputValue}
            onChangeText={search.onChangeText}
            onSubmitEditing={search.onSubmit}
            onClear={search.onClear}
            placeholder="Search your library"
            theme={theme}
          />
        </View>

        {isLoading ? (
          <View style={styles.center}>
            <ActivityIndicator testID="add-tracks-loading" />
          </View>
        ) : (
          <FlatList
            data={tracks}
            keyExtractor={(t) => t.id}
            renderItem={renderItem}
            onEndReached={onEndReached}
            onEndReachedThreshold={0.5}
            keyboardShouldPersistTaps="handled"
            ListFooterComponent={
              isFetchingNextPage ? <ActivityIndicator style={styles.footerSpinner} /> : null
            }
            ListEmptyComponent={
              <View style={styles.center}>
                <Text variant="label" tone="secondary">
                  {search.hasQuery ? 'No tracks match that search' : 'No tracks in your library yet'}
                </Text>
              </View>
            }
          />
        )}

        <View
          style={[
            styles.footer,
            { borderTopColor: theme.color.border, paddingBottom: insets.bottom + spacing.md },
          ]}
        >
          <Pressable
            testID="add-tracks-select-all"
            onPress={() => (allSelected ? selection.clear() : selection.selectAll(addable.map((t) => t.id)))}
            disabled={addable.length === 0}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={allSelected ? 'Deselect all tracks' : 'Select all tracks'}
          >
            <Text
              variant="label"
              style={{
                color: addable.length === 0 ? theme.color.textTertiary : theme.color.accent,
              }}
            >
              {allSelected ? 'Deselect all' : 'Select all'}
            </Text>
          </Pressable>

          <Pressable
            testID="add-tracks-confirm"
            onPress={confirm}
            disabled={selection.count === 0 || adding}
            accessibilityRole="button"
            accessibilityLabel={`Add ${selection.count} tracks to ${playlistName}`}
            accessibilityState={{ disabled: selection.count === 0 || adding }}
            style={({ pressed }) => [
              styles.confirm,
              {
                backgroundColor:
                  selection.count === 0 || adding ? theme.color.surface2 : theme.color.accent,
              },
              pressed && selection.count > 0 && !adding ? styles.pressed : null,
            ]}
          >
            {adding ? (
              <ActivityIndicator size="small" color={theme.color.onAccent} />
            ) : (
              <Text
                variant="label"
                style={{
                  color: selection.count === 0 ? theme.color.textTertiary : theme.color.onAccent,
                }}
              >
                {selection.count === 0
                  ? 'Select tracks'
                  : `Add ${selection.count} ${selection.count === 1 ? 'track' : 'tracks'}`}
              </Text>
            )}
          </Pressable>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.sm,
  },
  headerTitle: { flex: 1 },
  search: { paddingHorizontal: spacing.lg, paddingBottom: spacing.md },
  center: { paddingTop: spacing['3xl'], alignItems: 'center' },
  footerSpinner: { paddingVertical: spacing.lg },
  alreadyRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
  },
  alreadyTitle: { flex: 1 },
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.lg,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.md,
  },
  confirm: {
    minHeight: minInteractiveHeight,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    borderRadius: radius.full,
  },
  pressed: { opacity: 0.7 },
});
