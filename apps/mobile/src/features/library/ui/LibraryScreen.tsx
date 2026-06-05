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

import { useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, StyleSheet, View } from 'react-native';

import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';

import { LibraryRow } from './LibraryRow';
import { useLibrary } from '../hooks/useLibrary';
import { _viewForState } from '../state';
import { SignOutButton } from '../../auth/ui/SignOutButton';
import type { TrackResponse } from '../../../shared/api-client/types';
import { setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '@shared/api-client/discovery';

const _keyExtractor = (track: TrackResponse): string => track.id;
const SKELETON_ROWS = [0, 1, 2, 3, 4, 5, 6, 7];

type LibraryListProps = {
  items: TrackResponse[];
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isRefetching: boolean;
  onEndReached: () => void;
  onRefresh: () => void;
  onTrackPress: (track: TrackResponse) => void;
};

function LibraryList({
  items,
  hasNextPage,
  isFetchingNextPage,
  isRefetching,
  onEndReached,
  onRefresh,
  onTrackPress,
}: LibraryListProps): ReactElement {
  const theme = useTheme();

  return (
    <FlatList
      data={items}
      keyExtractor={_keyExtractor}
      renderItem={({ item }) => (
        <LibraryRow track={item} onPress={() => onTrackPress(item)} />
      )}
      onEndReached={hasNextPage ? onEndReached : undefined}
      onEndReachedThreshold={0.5}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
      refreshControl={
        <RefreshControl
          refreshing={isRefetching}
          onRefresh={onRefresh}
          tintColor={theme.color.accent}
          colors={[theme.color.accent]}
        />
      }
      ListFooterComponent={
        isFetchingNextPage ? (
          <View style={styles.footer}>
            <ActivityIndicator color={theme.color.accent} />
          </View>
        ) : null
      }
    />
  );
}

function _LibraryHeader(): ReactElement {
  return (
    <View style={styles.header}>
      <Text variant="displayL">Library</Text>
      <SignOutButton />
    </View>
  );
}

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const state = useLibrary();
  const view = _viewForState(state);

  const onTrackPress = (track: TrackResponse): void => {
    const result: DiscoveryResult = {
      kind: 'track',
      title: track.title,
      subtitle: track.artist,
      image_url: track.artwork_url ?? null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
    setDetailHandoff(result);
    router.push('/detail');
  };

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
        <Button testID="library-retry" label="Retry" onPress={state.refetch} />
      </View>
    );
  } else if (view === 'empty') {
    body = (
      <View testID="library-empty" style={styles.center}>
        <Text variant="title">Your library is empty</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Tracks you add will show up here.
        </Text>
        <Button label="Discover Music" onPress={() => router.push('/discover')} />
      </View>
    );
  } else {
    body = (
      <LibraryList
        items={state.items}
        hasNextPage={state.hasNextPage}
        isFetchingNextPage={state.isFetchingNextPage}
        isRefetching={state.isRefetching}
        onEndReached={state.fetchNextPage}
        onRefresh={state.refetch}
        onTrackPress={onTrackPress}
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
  footer: { paddingVertical: spacing.lg, alignItems: 'center' },
});
