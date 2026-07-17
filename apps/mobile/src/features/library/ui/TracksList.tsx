import type { ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { Text, spacing } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { LibraryRow } from './LibraryRow';

type TracksListProps = {
  tracks: TrackResponse[];
  emptyLabel: string;
  onPlay: (track: TrackResponse) => void;
  onPress: (track: TrackResponse) => void;
  onMore: (track: TrackResponse, anchor: MenuAnchor) => void;
  onRetry: (track: TrackResponse) => void;
  retryingTrackId: string | undefined;
  isPlaying: (trackId: string) => boolean;
};

export function TracksList({
  tracks,
  emptyLabel,
  onPlay,
  onPress,
  onMore,
  onRetry,
  retryingTrackId,
  isPlaying,
}: TracksListProps): ReactElement {
  return (
    <FlatList
      testID="library-tracks-list"
      data={tracks}
      keyExtractor={(t) => t.id}
      showsVerticalScrollIndicator={false}
      contentContainerStyle={tracks.length === 0 ? styles.emptyList : styles.list}
      ListEmptyComponent={
        <View style={styles.empty}>
          <Text variant="body" tone="secondary">
            {emptyLabel}
          </Text>
        </View>
      }
      renderItem={({ item }) => (
        <LibraryRow
          track={item}
          {...(item.acquisition_status === 'ready' ? { onPlay: () => onPlay(item) } : {})}
          onPress={() => onPress(item)}
          onMore={(anchor) => onMore(item, anchor)}
          {...(item.acquisition_status === 'failed' ? { onRetry: () => onRetry(item) } : {})}
          retrying={retryingTrackId === item.id}
          isPlaying={isPlaying(item.id)}
        />
      )}
    />
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
  emptyList: { flexGrow: 1 },
  empty: { flex: 1, alignItems: 'center', paddingTop: spacing['3xl'] },
});
