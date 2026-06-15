import { useState, type ReactElement } from 'react';
import { FlatList, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';

import { Screen, spacing } from '@shared/ui';

import { useLibraryHome } from '../hooks/useLibraryHome';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { useLibraryNavigation } from './useLibraryNavigation';
import { ExpandedHeader } from './ExpandedHeader';
import { LibraryRow } from './LibraryRow';
import { sortTracks, TRACK_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

export function AllTracksScreen(): ReactElement {
  const router = useRouter();
  const state = useLibraryHome();
  const { navigateToTrack } = useLibraryNavigation(router);
  const retryMutation = useRetryAcquisition();
  const retryingTrackId = retryMutation.isPending ? retryMutation.variables : undefined;
  const [sortKey, setSortKey] = useState<SortKey>('recent');

  const sorted = sortTracks(state.allTracks, sortKey);

  return (
    <Screen>
      <ExpandedHeader
        title="Recently Added"
        onBack={() => router.back()}
        sortKey={sortKey}
        onSortChange={setSortKey}
        sortOptions={TRACK_SORT_OPTIONS}
      />
      <FlatList
        data={sorted}
        keyExtractor={(t) => t.id}
        renderItem={({ item }) => (
          <LibraryRow
            track={item}
            onPress={() => navigateToTrack(item)}
            onRetry={
              item.acquisition_status === 'failed'
                ? () => retryMutation.mutate(item.id)
                : undefined
            }
            retrying={retryingTrackId === item.id}
          />
        )}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
});
