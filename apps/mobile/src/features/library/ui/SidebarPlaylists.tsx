import { useRouter } from 'expo-router';
import { Pressable, View } from 'react-native';

import type { PlaylistResponse } from '@shared/api-client/types';
import { asyncView } from '@shared/lib/async-view';
import { Text, type Theme, useTheme } from '@shared/ui';
import { sidebarItemStyle } from '@shared/ui/navigation/sidebarItemStyle';

import { usePlaylistActions } from '../hooks/usePlaylistActions';

function sidebarPlaylistRowProps(playlist: PlaylistResponse, onPress: () => void, theme: Theme) {
  return {
    testID: `sidebar-playlist-${playlist.id}`,
    onPress,
    accessibilityRole: 'link' as const,
    accessibilityLabel: playlist.name,
    style: sidebarItemStyle(theme, false),
  };
}

function SidebarPlaylistLabel({ name, color }: { name: string; color: string }) {
  return (
    <Text variant="label" style={{ color }} numberOfLines={1}>
      {name}
    </Text>
  );
}

function SidebarPlaylistRow({ playlist }: { playlist: PlaylistResponse }) {
  const router = useRouter();
  const theme = useTheme();
  const onPress = () => router.push(`/library/playlist/${playlist.id}`);
  return (
    <Pressable {...sidebarPlaylistRowProps(playlist, onPress, theme)}>
      <SidebarPlaylistLabel name={playlist.name} color={theme.color.textSecondary} />
    </Pressable>
  );
}

function SidebarPlaylistsEmptyHint() {
  const theme = useTheme();
  return (
    <Text testID="sidebar-playlists-empty" variant="caption" style={{ color: theme.color.textTertiary }}>
      No playlists yet
    </Text>
  );
}

function SidebarPlaylistsLoadingHint() {
  const theme = useTheme();
  return (
    <Text testID="sidebar-playlists-loading" variant="caption" style={{ color: theme.color.textTertiary }}>
      Loading…
    </Text>
  );
}

function sidebarPlaylistsRetryProps(onRetry: () => void, theme: Theme) {
  return {
    testID: 'sidebar-playlists-retry',
    onPress: onRetry,
    accessibilityRole: 'button' as const,
    accessibilityLabel: 'Retry loading playlists',
    style: sidebarItemStyle(theme, false),
  };
}

function SidebarPlaylistsRetryButton({ onRetry, theme }: { onRetry: () => void; theme: Theme }) {
  return (
    <Pressable {...sidebarPlaylistsRetryProps(onRetry, theme)}>
      <Text variant="caption" style={{ color: theme.color.accent }}>
        Retry
      </Text>
    </Pressable>
  );
}

function SidebarPlaylistsErrorMessage({ color }: { color: string }) {
  return (
    <Text variant="caption" style={{ color }}>
      Couldn&apos;t load playlists.
    </Text>
  );
}

function SidebarPlaylistsErrorHint({ onRetry }: { onRetry: () => void }) {
  const theme = useTheme();
  return (
    <View testID="sidebar-playlists-error">
      <SidebarPlaylistsErrorMessage color={theme.color.textTertiary} />
      <SidebarPlaylistsRetryButton onRetry={onRetry} theme={theme} />
    </View>
  );
}

function SidebarPlaylistRows({ playlists }: { playlists: PlaylistResponse[] }) {
  return (
    <View>
      {playlists.map((playlist) => (
        <SidebarPlaylistRow key={playlist.id} playlist={playlist} />
      ))}
    </View>
  );
}

function sidebarPlaylistsView(playlists: PlaylistResponse[], isLoading: boolean, error: Error | null) {
  return asyncView({ isLoading, isError: error != null, isEmpty: playlists.length === 0 });
}

export function SidebarPlaylists() {
  const { playlists, playlistsError, isLoadingPlaylists, refetchPlaylists } = usePlaylistActions();
  const view = sidebarPlaylistsView(playlists, isLoadingPlaylists, playlistsError);

  if (view === 'loading') return <SidebarPlaylistsLoadingHint />;
  if (view === 'error') return <SidebarPlaylistsErrorHint onRetry={refetchPlaylists} />;
  if (view === 'empty') return <SidebarPlaylistsEmptyHint />;
  return <SidebarPlaylistRows playlists={playlists} />;
}
