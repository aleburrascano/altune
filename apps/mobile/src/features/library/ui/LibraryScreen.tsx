import { useRouter } from 'expo-router';
import { useCallback, useState, type ReactElement } from 'react';
import { FlatList, Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { setDetailHandoff } from '@shared/lib/detail-handoff';
import { createPlaylist, getPlaylists } from '@shared/api-client/playlists';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';
import { Button, Chip, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';

import { AddToPlaylistSheet } from '@shared/ui/AddToPlaylistSheet';

import { AlbumCarousel } from './AlbumCarousel';
import { ArtistCarousel } from './ArtistCarousel';
import { CreatePlaylistModal } from './CreatePlaylistModal';
import { LibraryRow } from './LibraryRow';
import { PlaylistCarousel } from './PlaylistCarousel';
import { ProfileSheet } from './ProfileSheet';
import { useLibraryHome } from '../hooks/useLibraryHome';
import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';
import { useSession } from '../../auth/hooks/useSession';

type ExpandedSection = 'recent' | 'albums' | 'artists' | null;
type SortKey = 'recent' | 'az' | 'year';

function SectionHeader({
  title,
  onSeeAll,
  testID,
}: {
  title: string;
  onSeeAll?: () => void;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  return (
    <View testID={testID} style={styles.sectionHeader}>
      <Text variant="title">{title}</Text>
      {onSeeAll != null ? (
        <Pressable onPress={onSeeAll} hitSlop={8}>
          <Text variant="label" style={{ color: theme.color.accent }}>
            See All →
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function ExpandedHeader({
  title,
  onCollapse,
  sortKey,
  onSortChange,
  sortOptions,
}: {
  title: string;
  onCollapse: () => void;
  sortKey: SortKey;
  onSortChange: (key: SortKey) => void;
  sortOptions: { key: SortKey; label: string }[];
}): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.expandedHeader}>
      <View style={styles.expandedTitleRow}>
        <Text variant="title">{title}</Text>
        <Pressable onPress={onCollapse} hitSlop={8}>
          <Text variant="label" style={{ color: theme.color.accent }}>
            Collapse ↑
          </Text>
        </Pressable>
      </View>
      <View style={styles.sortRow}>
        {sortOptions.map((opt) => (
          <Chip
            key={opt.key}
            label={opt.label}
            selected={sortKey === opt.key}
            onPress={() => onSortChange(opt.key)}
            testID={`sort-${opt.key}`}
          />
        ))}
      </View>
    </View>
  );
}

function LibraryHeader({
  initial,
  onAvatarPress,
}: {
  initial: string;
  onAvatarPress?: () => void;
}): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.header}>
      <Text variant="displayL">Library</Text>
      <Pressable
        testID="library-avatar"
        onPress={onAvatarPress}
        accessibilityRole="button"
        accessibilityLabel="Profile"
        hitSlop={8}
      >
        <View style={[styles.avatar, { backgroundColor: theme.color.accent }]}>
          <Text variant="bodyStrong" tone="onAccent">
            {initial}
          </Text>
        </View>
      </Pressable>
    </View>
  );
}

function sortAlbums(albums: AlbumGroup[], key: SortKey): AlbumGroup[] {
  const sorted = [...albums];
  switch (key) {
    case 'recent':
      return sorted.sort((a, b) => b.mostRecentAddedAt.localeCompare(a.mostRecentAddedAt));
    case 'az':
      return sorted.sort((a, b) => a.album.localeCompare(b.album));
    case 'year':
      return sorted.sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
  }
}

function sortArtists(artists: ArtistGroup[], key: SortKey): ArtistGroup[] {
  const sorted = [...artists];
  switch (key) {
    case 'recent':
      return sorted.sort((a, b) => b.mostRecentAddedAt.localeCompare(a.mostRecentAddedAt));
    case 'az':
      return sorted.sort((a, b) => a.artist.localeCompare(b.artist));
    case 'year':
      return sorted;
  }
}

function sortTracks(tracks: TrackResponse[], key: SortKey): TrackResponse[] {
  const sorted = [...tracks];
  switch (key) {
    case 'recent':
      return sorted.sort((a, b) => b.added_at.localeCompare(a.added_at));
    case 'az':
      return sorted.sort((a, b) => a.title.localeCompare(b.title));
    case 'year':
      return sorted.sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
  }
}

const ALBUM_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
  { key: 'year', label: 'Year' },
];

const ARTIST_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
];

const TRACK_SORT_OPTIONS: { key: SortKey; label: string }[] = [
  { key: 'recent', label: 'Recent' },
  { key: 'az', label: 'A–Z' },
];

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const state = useLibraryHome();
  const sessionState = useSession();
  const [expanded, setExpanded] = useState<ExpandedSection>(null);
  const [sortKey, setSortKey] = useState<SortKey>('recent');
  const [profileVisible, setProfileVisible] = useState(false);
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);

  const queryClient = useQueryClient();
  const playlistsQuery = useQuery({
    queryKey: ['playlists'],
    queryFn: getPlaylists,
  });
  const playlists = playlistsQuery.data?.items ?? [];

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

  const navigateToTrack = useCallback(
    (track: TrackResponse): void => {
      const result: DiscoveryResult = {
        kind: 'track',
        title: track.title,
        subtitle: track.artist,
        image_url: track.artwork_url ?? null,
        confidence: 'high',
        sources: [],
        extras: {
          ...(track.album != null ? { album: track.album } : {}),
          ...(track.duration_seconds != null ? { duration_seconds: track.duration_seconds } : {}),
        },
      };
      setDetailHandoff(result);
      router.push('/library/detail');
    },
    [router],
  );

  const navigateToAlbum = useCallback(
    (album: AlbumGroup): void => {
      const result: DiscoveryResult = {
        kind: 'album',
        title: album.album,
        subtitle: album.artist,
        image_url: album.artworkUrl,
        confidence: 'high',
        sources: [],
        extras: {
          ...(album.year != null ? { year: album.year } : {}),
          track_count: album.trackCount,
        },
      };
      setDetailHandoff(result);
      router.push('/library/detail');
    },
    [router],
  );

  const navigateToArtist = useCallback(
    (artist: ArtistGroup): void => {
      const result: DiscoveryResult = {
        kind: 'artist',
        title: artist.artist,
        subtitle: null,
        image_url: artist.artworkUrl,
        confidence: 'high',
        sources: [],
        extras: {},
      };
      setDetailHandoff(result);
      router.push('/library/detail');
    },
    [router],
  );

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

  // -- Expanded: Recently Added (all tracks) --
  if (expanded === 'recent') {
    const sorted = sortTracks(state.allTracks, sortKey);
    return (
      <Screen>
        <LibraryHeader initial={initial} onAvatarPress={() => setProfileVisible(true)} />
        <ExpandedHeader
          title="Recently Added"
          onCollapse={collapse}
          sortKey={sortKey}
          onSortChange={setSortKey}
          sortOptions={TRACK_SORT_OPTIONS}
        />
        <FlatList
          data={sorted}
          keyExtractor={(t) => t.id}
          renderItem={({ item }) => (
            <LibraryRow track={item} onPress={() => navigateToTrack(item)} onLongPress={() => setAddToPlaylistTrack(item)} />
          )}
          showsVerticalScrollIndicator={false}
          contentContainerStyle={styles.expandedList}
        />
        <ProfileSheet visible={profileVisible} email={email} onClose={() => setProfileVisible(false)} />
      </Screen>
    );
  }

  // -- Expanded: Albums (grid) --
  if (expanded === 'albums') {
    const sorted = sortAlbums(state.albums, sortKey);
    return (
      <Screen>
        <LibraryHeader initial={initial} onAvatarPress={() => setProfileVisible(true)} />
        <ExpandedHeader
          title="Albums"
          onCollapse={collapse}
          sortKey={sortKey}
          onSortChange={setSortKey}
          sortOptions={ALBUM_SORT_OPTIONS}
        />
        <FlatList
          data={sorted}
          keyExtractor={(a) => a.key}
          numColumns={2}
          columnWrapperStyle={styles.albumGridRow}
          contentContainerStyle={styles.expandedList}
          showsVerticalScrollIndicator={false}
          renderItem={({ item }) => (
            <Pressable
              style={styles.albumGridItem}
              onPress={() => navigateToAlbum(item)}
              accessibilityRole="button"
              accessibilityLabel={`${item.album} by ${item.artist}`}
            >
              <View style={styles.albumGridCover}>
                <View style={{ width: '100%', aspectRatio: 1 }}>
                  {/* Using inline Image to avoid Artwork's fixed size */}
                  <View
                    style={{
                      width: '100%',
                      height: '100%',
                      borderRadius: 8,
                      backgroundColor: theme.color.surface2,
                      overflow: 'hidden',
                    }}
                  >
                    {item.artworkUrl != null ? (
                      <ExpoImage source={{ uri: item.artworkUrl }} style={{ width: '100%', height: '100%' }} contentFit="cover" />
                    ) : null}
                  </View>
                </View>
              </View>
              <Text variant="label" numberOfLines={1}>
                {item.album}
              </Text>
              <Text variant="caption" tone="secondary" numberOfLines={1}>
                {item.artist}
                {item.year != null ? ` · ${item.year}` : ''}
              </Text>
            </Pressable>
          )}
        />
        <ProfileSheet visible={profileVisible} email={email} onClose={() => setProfileVisible(false)} />
      </Screen>
    );
  }

  // -- Expanded: Artists (grid) --
  if (expanded === 'artists') {
    const sorted = sortArtists(state.artists, sortKey);
    return (
      <Screen>
        <LibraryHeader initial={initial} onAvatarPress={() => setProfileVisible(true)} />
        <ExpandedHeader
          title="Artists"
          onCollapse={collapse}
          sortKey={sortKey}
          onSortChange={setSortKey}
          sortOptions={ARTIST_SORT_OPTIONS}
        />
        <FlatList
          data={sorted}
          keyExtractor={(a) => a.key}
          numColumns={3}
          columnWrapperStyle={styles.artistGridRow}
          contentContainerStyle={styles.expandedList}
          showsVerticalScrollIndicator={false}
          renderItem={({ item }) => (
            <Pressable
              style={styles.artistGridItem}
              onPress={() => navigateToArtist(item)}
              accessibilityRole="button"
              accessibilityLabel={item.artist}
            >
              <View
                style={{
                  width: 80,
                  height: 80,
                  borderRadius: 40,
                  backgroundColor: theme.color.surface2,
                  overflow: 'hidden',
                }}
              >
                {item.artworkUrl != null ? (
                  <ExpoImage source={{ uri: item.artworkUrl }} style={{ width: 80, height: 80 }} contentFit="cover" />
                ) : null}
              </View>
              <Text variant="caption" numberOfLines={1} style={styles.artistGridName}>
                {item.artist}
              </Text>
            </Pressable>
          )}
        />
        <ProfileSheet visible={profileVisible} email={email} onClose={() => setProfileVisible(false)} />
      </Screen>
    );
  }

  // -- Default: Sectioned Home --
  return (
    <Screen>
      <LibraryHeader initial={initial} onAvatarPress={() => setProfileVisible(true)} />
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scroll}
        refreshControl={
          <RefreshControl
            refreshing={false}
            onRefresh={state.refetch}
            tintColor={theme.color.accent}
            colors={[theme.color.accent]}
          />
        }
      >
        <SectionHeader title="Playlists" />
        <PlaylistCarousel
          playlists={playlists}
          onPlaylistPress={(pl) => router.push(`/library/playlist/${pl.id}` as never)}
          onCreatePress={() => setCreateModalVisible(true)}
        />

        {state.recentTracks.length > 0 ? (
          <>
            <SectionHeader
              title="Recently Added"
              onSeeAll={() => setExpanded('recent')}
              testID="library-section-recent"
            />
            {state.recentTracks.map((track) => (
              <LibraryRow key={track.id} track={track} onPress={() => navigateToTrack(track)} onLongPress={() => setAddToPlaylistTrack(track)} />
            ))}
          </>
        ) : null}

        {state.albums.length > 0 ? (
          <>
            <SectionHeader
              title="Albums"
              onSeeAll={() => setExpanded('albums')}
              testID="library-section-albums"
            />
            <AlbumCarousel albums={state.albums} onAlbumPress={navigateToAlbum} />
          </>
        ) : null}

        {state.artists.length > 0 ? (
          <>
            <SectionHeader
              title="Artists"
              onSeeAll={() => setExpanded('artists')}
              testID="library-section-artists"
            />
            <ArtistCarousel artists={state.artists} onArtistPress={navigateToArtist} />
          </>
        ) : null}
      </ScrollView>
      <ProfileSheet
        visible={profileVisible}
        email={email}
        onClose={() => setProfileVisible(false)}
      />
      <CreatePlaylistModal
        visible={createModalVisible}
        onClose={() => setCreateModalVisible(false)}
        onCreate={(name) => createMutation.mutate(name)}
        loading={createMutation.isPending}
      />
      <AddToPlaylistSheet
        visible={addToPlaylistTrack != null}
        trackId={addToPlaylistTrack?.id ?? ''}
        trackTitle={addToPlaylistTrack != null ? `${addToPlaylistTrack.title} — ${addToPlaylistTrack.artist}` : ''}
        onClose={() => setAddToPlaylistTrack(null)}
      />
    </Screen>
  );
}

// Lazy import to avoid pulling expo-image into non-expanded renders
const { Image: ExpoImage } = require('expo-image') as { Image: typeof import('expo-image').Image };

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  avatar: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.xl,
    paddingBottom: spacing.sm,
  },
  expandedHeader: { paddingBottom: spacing.sm },
  expandedTitleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingBottom: spacing.sm,
  },
  sortRow: { flexDirection: 'row', gap: spacing.sm },
  body: { flex: 1 },
  scroll: { paddingBottom: spacing['3xl'] },
  expandedList: { paddingBottom: spacing['3xl'] },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
  skeletonSection: { gap: spacing.md, paddingTop: spacing.xl },
  skeletonCarousel: { flexDirection: 'row', gap: spacing.md },
  skeletonRow: { flexDirection: 'row', gap: spacing.md, paddingVertical: spacing.sm },
  skeletonText: { flex: 1, gap: spacing.xs, justifyContent: 'center' },
  albumGridRow: { gap: spacing.md, paddingHorizontal: spacing.lg },
  albumGridItem: { flex: 1, marginBottom: spacing.lg },
  albumGridCover: { marginBottom: spacing.xs },
  artistGridRow: { gap: spacing.md, paddingHorizontal: spacing.lg, justifyContent: 'flex-start' },
  artistGridItem: { alignItems: 'center', marginBottom: spacing.lg, width: 88 },
  artistGridName: { textAlign: 'center', marginTop: spacing.xs, width: 80 },
});
