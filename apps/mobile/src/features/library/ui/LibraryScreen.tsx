import { useRouter } from 'expo-router';
import { useCallback, useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { createPlaylist, getPlaylists } from '@shared/api-client/playlists';
import type { TrackResponse } from '@shared/api-client/types';
import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';

import { useSession } from '../../auth/hooks/useSession';
import { useLibraryHome } from '../hooks/useLibraryHome';
import { ExpandedAlbums } from './ExpandedAlbums';
import { ExpandedArtists } from './ExpandedArtists';
import { ExpandedTracks } from './ExpandedTracks';
import { LibraryHeader } from './LibraryHeader';
import { LibraryHome } from './LibraryHome';
import type { SortKey } from './sort';
import { useLibraryNavigation } from './useLibraryNavigation';

type ExpandedSection = 'recent' | 'albums' | 'artists' | null;

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const state = useLibraryHome();
  const sessionState = useSession();
  const { navigateToTrack, navigateToAlbum, navigateToArtist } = useLibraryNavigation(router);

  const [expanded, setExpanded] = useState<ExpandedSection>(null);
  const [sortKey, setSortKey] = useState<SortKey>('recent');
  const [profileVisible, setProfileVisible] = useState(false);
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

  const email =
    sessionState.status === 'signed-in' ? (sessionState.session.user.email ?? '') : '';
  const initial = email.length > 0 ? email[0]!.toUpperCase() : '?';

  const collapse = useCallback(() => {
    setExpanded(null);
    setSortKey('recent');
  }, []);

  if (state.isLoading) {
    return (
      <Screen>
        <LibraryHeader initial={initial} />
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
        <LibraryHeader initial={initial} />
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
        <LibraryHeader initial={initial} />
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

  if (expanded === 'recent') {
    return (
      <ExpandedTracks
        tracks={state.allTracks}
        sortKey={sortKey}
        onSortChange={setSortKey}
        onCollapse={collapse}
        navigateToTrack={navigateToTrack}
        onLongPress={setAddToPlaylistTrack}
        initial={initial}
        email={email}
        profileVisible={profileVisible}
        onProfileToggle={setProfileVisible}
      />
    );
  }

  if (expanded === 'albums') {
    return (
      <ExpandedAlbums
        albums={state.albums}
        sortKey={sortKey}
        onSortChange={setSortKey}
        onCollapse={collapse}
        navigateToAlbum={navigateToAlbum}
        initial={initial}
        email={email}
        profileVisible={profileVisible}
        onProfileToggle={setProfileVisible}
      />
    );
  }

  if (expanded === 'artists') {
    return (
      <ExpandedArtists
        artists={state.artists}
        sortKey={sortKey}
        onSortChange={setSortKey}
        onCollapse={collapse}
        navigateToArtist={navigateToArtist}
        initial={initial}
        email={email}
        profileVisible={profileVisible}
        onProfileToggle={setProfileVisible}
      />
    );
  }

  return (
    <LibraryHome
      playlists={playlists}
      recentTracks={state.recentTracks}
      albums={state.albums}
      artists={state.artists}
      navigateToTrack={navigateToTrack}
      navigateToAlbum={navigateToAlbum}
      navigateToArtist={navigateToArtist}
      onExpandRecent={() => setExpanded('recent')}
      onExpandAlbums={() => setExpanded('albums')}
      onExpandArtists={() => setExpanded('artists')}
      onPlaylistPress={(pl) => router.push(`/library/playlist/${pl.id}` as never)}
      onRefresh={state.refetch}
      initial={initial}
      email={email}
      profileVisible={profileVisible}
      onProfileToggle={setProfileVisible}
      createModalVisible={createModalVisible}
      onCreateModalToggle={setCreateModalVisible}
      onCreatePlaylist={(name) => createMutation.mutate(name)}
      createLoading={createMutation.isPending}
      addToPlaylistTrack={addToPlaylistTrack}
      onAddToPlaylistTrackChange={setAddToPlaylistTrack}
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
