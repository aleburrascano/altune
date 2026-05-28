/**
 * LibraryScreen — paginated track list with designed empty + error states.
 *
 * The state-machine decision (which sub-view to render) lives in `../state.ts`
 * so it can be unit-tested as a pure function — the JSX branches just consume
 * the decision. Restyled onto the design system per ADR-0008.
 *
 * AC mapping:
 * - AC#1: FlatList renders title + artist per row, server-ordered.
 * - AC#3: FlatList onEndReached triggers fetchNextPage when hasNextPage.
 * - AC#5: empty state testID="library-empty".
 * - AC#6: error state testID="library-error" + retry testID="library-retry".
 */

import type { ReactElement } from 'react';
import { FlatList, StyleSheet, View, type ListRenderItem } from 'react-native';

import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';

import { LibraryRow } from './LibraryRow';
import { useLibrary } from '../hooks/useLibrary';
import { _viewForState } from '../state';
import { SignOutButton } from '../../auth/ui/SignOutButton';
import type { TrackResponse } from '../../../shared/api-client/types';

const _renderRow: ListRenderItem<TrackResponse> = ({ item }) => <LibraryRow track={item} />;
const _keyExtractor = (track: TrackResponse): string => track.id;
const SKELETON_ROWS = [0, 1, 2, 3, 4, 5, 6, 7];

function _LibraryHeader(): ReactElement {
  return (
    <View style={styles.header}>
      <Text variant="displayL">Library</Text>
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
      <View testID="library-loading" style={styles.list}>
        {SKELETON_ROWS.map((i) => (
          <View key={i} style={styles.skeletonRow}>
            <Skeleton width="60%" height={15} />
            <Skeleton width="35%" height={12} />
          </View>
        ))}
      </View>
    );
  } else if (view === 'error') {
    body = (
      <View testID="library-error" style={styles.center}>
        <Text variant="title">Couldn&apos;t load your library</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Check your connection and try again.
        </Text>
        <Button testID="library-retry" label="Retry" onPress={state.fetchNextPage} />
      </View>
    );
  } else if (view === 'empty') {
    body = (
      <View testID="library-empty" style={styles.center}>
        <Text variant="title">Your library is empty</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Tracks you add will show up here.
        </Text>
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
        contentContainerStyle={styles.listContent}
        showsVerticalScrollIndicator={false}
      />
    );
  }

  return (
    <Screen>
      <_LibraryHeader />
      {body}
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  list: { flex: 1 },
  listContent: { paddingBottom: spacing.xl },
  skeletonRow: { paddingVertical: spacing.md, gap: spacing.sm },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
});
