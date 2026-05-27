/**
 * LibraryScreen — paginated track list with designed empty + error states.
 *
 * The state-machine decision (which sub-view to render) lives in
 * `../state.ts` so it can be unit-tested without pulling React Native into
 * jest.
 *
 * AC mapping:
 * - AC#1: FlatList renders title + artist per row, server-ordered.
 * - AC#3: FlatList onEndReached triggers the hook's fetchNextPage when
 *   hasNextPage; the hook stops when has_more=false.
 * - AC#5: empty state with testID="library-empty" and visible text.
 * - AC#6: error state with testID="library-error" + retry button with
 *   testID="library-retry".
 */

import {
  ActivityIndicator,
  FlatList,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  type ListRenderItem,
} from 'react-native';

import { LibraryRow } from './LibraryRow';
import { useLibrary } from '../hooks/useLibrary';
import { _viewForState } from '../state';
import type { TrackResponse } from '../../../shared/api-client/types';

const _renderRow: ListRenderItem<TrackResponse> = ({ item }) => <LibraryRow track={item} />;
const _keyExtractor = (track: TrackResponse): string => track.id;

export function LibraryScreen(): JSX.Element {
  const state = useLibrary();
  const view = _viewForState(state);

  if (view === 'loading') {
    return (
      <View style={styles.center} testID="library-loading">
        <ActivityIndicator color="#fff" />
      </View>
    );
  }

  if (view === 'error') {
    return (
      <View style={styles.center} testID="library-error">
        <Text style={styles.errorText}>Couldn&apos;t load your library.</Text>
        <TouchableOpacity
          onPress={state.fetchNextPage}
          testID="library-retry"
          style={styles.retryButton}
        >
          <Text style={styles.retryText}>Retry</Text>
        </TouchableOpacity>
      </View>
    );
  }

  if (view === 'empty') {
    return (
      <View style={styles.center} testID="library-empty">
        <Text style={styles.emptyText}>Your library is empty.</Text>
        <Text style={styles.emptyHint}>Tracks you add will show up here.</Text>
      </View>
    );
  }

  return (
    <View style={styles.list}>
      <FlatList
        data={state.items}
        keyExtractor={_keyExtractor}
        renderItem={_renderRow}
        onEndReached={state.hasNextPage ? state.fetchNextPage : undefined}
        onEndReachedThreshold={0.5}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  list: {
    flex: 1,
    backgroundColor: '#000',
  },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
    backgroundColor: '#000',
  },
  errorText: {
    color: '#fff',
    fontSize: 16,
    marginBottom: 16,
  },
  retryButton: {
    paddingVertical: 10,
    paddingHorizontal: 24,
    borderRadius: 8,
    backgroundColor: '#1f1f1f',
  },
  retryText: {
    color: '#fff',
    fontSize: 14,
    fontWeight: '500',
  },
  emptyText: {
    color: '#fff',
    fontSize: 18,
    fontWeight: '500',
    marginBottom: 8,
  },
  emptyHint: {
    color: '#888',
    fontSize: 14,
    textAlign: 'center',
  },
});
