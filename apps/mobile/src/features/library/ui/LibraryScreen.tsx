/**
 * LibraryScreen — paginated track list with designed empty + error states.
 *
 * The state-machine decision (which sub-view to render) lives in
 * `../state.ts` so it can be unit-tested as a pure function — the JSX
 * branches just consume the decision.
 *
 * AC mapping:
 * - AC#1: FlatList renders title + artist per row, server-ordered.
 * - AC#3: FlatList onEndReached triggers the hook's fetchNextPage when
 *   hasNextPage; the hook stops when has_more=false.
 * - AC#5: empty state with testID="library-empty" and visible text.
 * - AC#6: error state with testID="library-error" + retry button with
 *   testID="library-retry".
 */

import type { ReactElement } from 'react';
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
import { SignOutButton } from '../../auth/ui/SignOutButton';
import type { TrackResponse } from '../../../shared/api-client/types';

const _renderRow: ListRenderItem<TrackResponse> = ({ item }) => <LibraryRow track={item} />;
const _keyExtractor = (track: TrackResponse): string => track.id;

function _LibraryHeader(): ReactElement {
  return (
    <View style={styles.header}>
      <Text style={styles.headerTitle}>Library</Text>
      <SignOutButton />
    </View>
  );
}

export function LibraryScreen(): ReactElement {
  const state = useLibrary();
  const view = _viewForState(state);

  let body: ReactElement;
  if (view === 'loading') {
    body = (
      <View style={styles.center} testID="library-loading">
        <ActivityIndicator color="#fff" />
      </View>
    );
  } else if (view === 'error') {
    body = (
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
  } else if (view === 'empty') {
    body = (
      <View style={styles.center} testID="library-empty">
        <Text style={styles.emptyText}>Your library is empty.</Text>
        <Text style={styles.emptyHint}>Tracks you add will show up here.</Text>
      </View>
    );
  } else {
    body = (
      <FlatList
        data={state.items}
        keyExtractor={_keyExtractor}
        renderItem={_renderRow}
        onEndReached={state.hasNextPage ? state.fetchNextPage : undefined}
        onEndReachedThreshold={0.5}
      />
    );
  }

  return (
    <View style={styles.list}>
      <_LibraryHeader />
      {body}
    </View>
  );
}

const styles = StyleSheet.create({
  list: {
    flex: 1,
    backgroundColor: '#000',
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 16,
    paddingTop: 48,
    paddingBottom: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#222',
  },
  headerTitle: {
    color: '#fff',
    fontSize: 20,
    fontWeight: '600',
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
