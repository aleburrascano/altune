import { useRouter } from 'expo-router';
import { Pressable, View } from 'react-native';

import type { PlaylistResponse } from '@shared/api-client/types';
import { Text, spacing, useTheme } from '@shared/ui';

import { usePlaylistActions } from '../hooks/usePlaylistActions';

function sidebarPlaylistRowProps(playlist: PlaylistResponse, onPress: () => void) {
  return {
    testID: `sidebar-playlist-${playlist.id}`,
    onPress,
    accessibilityRole: 'link' as const,
    accessibilityLabel: playlist.name,
    style: { paddingVertical: spacing.xs },
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
    <Pressable {...sidebarPlaylistRowProps(playlist, onPress)}>
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

function SidebarPlaylistRows({ playlists }: { playlists: PlaylistResponse[] }) {
  return (
    <View>
      {playlists.map((playlist) => (
        <SidebarPlaylistRow key={playlist.id} playlist={playlist} />
      ))}
    </View>
  );
}

export function SidebarPlaylists() {
  const { playlists } = usePlaylistActions();
  if (playlists.length === 0) return <SidebarPlaylistsEmptyHint />;
  return <SidebarPlaylistRows playlists={playlists} />;
}
