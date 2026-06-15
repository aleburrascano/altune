import { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { createPlaylist, getPlaylists } from '@shared/api-client/playlists';
import type { TrackResponse } from '@shared/api-client/types';
import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { useLibraryHome } from '../hooks/useLibraryHome';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { LibraryHeader } from './LibraryHeader';
import { LibraryHome } from './LibraryHome';
import { useLibraryNavigation } from './useLibraryNavigation';

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const state = useLibraryHome();
  const { navigateToTrack, navigateToAlbum, navigateToArtist } = useLibraryNavigation(router);
  const deleteMutation = useDeleteTrack();
  const retryMutation = useRetryAcquisition();

  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);

  const queryClient = useQueryClient();
  const { data: playlistsData } = useQuery({
    queryKey: ['playlists'],
    queryFn: getPlaylists,
  });
  const playlists = playlistsData?.items ?? [];

  const createMutation = useMutation({
    mutationFn: (name: string) => createPlaylist({ name }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      setCreateModalVisible(false);
    },
  });

  if (state.isLoading) {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-loading" style={styles.body}>
          <View style={styles.skeletonSection}>
            <Skeleton width={120} height={16} />
            <View style={styles.skeletonCarousel}>
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} width={110} height={110} radius={8} />
              ))}
            </View>
          </View>
          {[0, 1, 2, 3].map((i) => (
            <View key={i} style={styles.skeletonRow}>
              <Skeleton width={48} height={48} radius={6} />
              <View style={styles.skeletonText}>
                <Skeleton width="70%" height={14} />
                <Skeleton width="50%" height={12} />
              </View>
            </View>
          ))}
        </View>
      </Screen>
    );
  }

  if (state.error != null) {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-error" style={styles.center}>
          <Text variant="title">Couldn&apos;t load your library</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            Check your connection and try again.
          </Text>
          <Button testID="library-retry" label="Retry" onPress={state.refetch} />
        </View>
      </Screen>
    );
  }

  if (state.total === 0) {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-empty" style={styles.center}>
          <Text variant="title">Your library is empty</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            Tracks you add will show up here.
          </Text>
          <Button label="Discover Music" onPress={() => router.push('/discover')} />
        </View>
      </Screen>
    );
  }

  const retryingTrackId = retryMutation.isPending ? retryMutation.variables : undefined;

  return (
    <LibraryHome
      playlists={playlists}
      recentTracks={state.recentTracks}
      albums={state.albums}
      artists={state.artists}
      navigateToTrack={navigateToTrack}
      navigateToAlbum={navigateToAlbum}
      navigateToArtist={navigateToArtist}
      onExpandRecent={() => router.push('/library/all-tracks' as never)}
      onExpandAlbums={() => router.push('/library/all-albums' as never)}
      onExpandArtists={() => router.push('/library/all-artists' as never)}
      onPlaylistPress={(pl) => router.push(`/library/playlist/${pl.id}` as never)}
      onRefresh={state.refetch}
      playlistActions={{
        createModalVisible,
        onCreateModalToggle: setCreateModalVisible,
        onCreatePlaylist: (name) => createMutation.mutate(name),
        createLoading: createMutation.isPending,
        addToPlaylistTrack,
        onAddToPlaylistTrackChange: setAddToPlaylistTrack,
      }}
      trackActions={{
        onDeleteTrack: (trackId) => deleteMutation.mutate(trackId),
        onRetryTrack: (trackId) => retryMutation.mutate(trackId),
        retryingTrackId,
      }}
    />
  );
}

const styles = StyleSheet.create({
  body: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
  skeletonSection: { gap: spacing.md, paddingTop: spacing.xl },
  skeletonCarousel: { flexDirection: 'row', gap: spacing.md },
  skeletonRow: { flexDirection: 'row', gap: spacing.md, paddingVertical: spacing.sm },
  skeletonText: { flex: 1, gap: spacing.xs, justifyContent: 'center' },
});
