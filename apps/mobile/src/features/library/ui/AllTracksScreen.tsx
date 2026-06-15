import { useState, type ReactElement } from 'react';
import { FlatList, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';

import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { usePlayback } from '@shared/playback/usePlayback';
import { Screen, spacing } from '@shared/ui';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { useLibraryHome } from '../hooks/useLibraryHome';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { useLibraryNavigation } from './useLibraryNavigation';
import { AddToPlaylistSheet } from './AddToPlaylistSheet';
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
  const deleteMutation = useDeleteTrack();
  const playback = usePlayback();
  const [sortKey, setSortKey] = useState<SortKey>('recent');
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);
  const [actionTrack, setActionTrack] = useState<TrackResponse | null>(null);

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
            {...(item.acquisition_status === 'ready' ? { onPlay: () => {
              void playback.play({
                source: { kind: 'library', trackId: item.id },
                title: item.title,
                artist: item.artist,
                artworkUrl: item.artwork_url ?? null,
                durationSeconds: item.duration_seconds ?? undefined,
              });
            } } : {})}
            onPress={() => navigateToTrack(item)}
            onMore={() => setActionTrack(item)}
            {...(item.acquisition_status === 'failed' ? { onRetry: () => retryMutation.mutate(item.id) } : {})}
            retrying={retryingTrackId === item.id}
            isPlaying={isCurrentlyPlaying(playback, { kind: 'library', trackId: item.id })}
          />
        )}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
      />
      <AddToPlaylistSheet
        visible={addToPlaylistTrack != null}
        trackId={addToPlaylistTrack?.id ?? ''}
        trackTitle={addToPlaylistTrack != null ? `${addToPlaylistTrack.title} — ${addToPlaylistTrack.artist}` : ''}
        onClose={() => setAddToPlaylistTrack(null)}
      />
      <ActionSheet
        visible={actionTrack != null}
        title={actionTrack?.title}
        subtitle={actionTrack != null ? `${actionTrack.artist}${actionTrack.album != null ? ` · ${actionTrack.album}` : ''}` : undefined}
        options={actionTrack != null ? [
          { label: 'View Details', onPress: () => navigateToTrack(actionTrack) },
          { label: 'Add to Playlist', onPress: () => setAddToPlaylistTrack(actionTrack) },
          { label: 'Remove from Library', tone: 'danger' as const, onPress: () => deleteMutation.mutate(actionTrack.id) },
        ] : []}
        onClose={() => setActionTrack(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
});
