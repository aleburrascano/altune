import { View } from 'react-native';
import type { PlaylistResponse } from '@shared/catalog';
import type { Navigator } from '@shared/navigation';
import {
  asyncView,
  Text,
  type Theme,
  useTheme,
  sidebarItemStyle,
  Tappable,
  usePressableInteraction,
} from '@shared/ui';
import { usePlaylistActions } from '@features/library/view/usePlaylistActions';

function sidebarPlaylistRowProps(playlist: PlaylistResponse, onPress: () => void) {
  return {
    action: 'sidebar.playlist.open',
    testID: `sidebar-playlist-${playlist.id}`,
    onPress,
    accessibilityRole: 'link' as const,
    accessibilityLabel: playlist.name,
  };
}

function SidebarPlaylistLabel({ name, color }: { name: string; color: string }) {
  return (
    <Text variant="label" style={{ color }} numberOfLines={1}>
      {name}
    </Text>
  );
}

type RowProps = { playlist: PlaylistResponse; navigator: Navigator };

function SidebarPlaylistRow({ playlist, navigator }: RowProps) {
  const theme = useTheme();
  function onPress(): void {
    navigator.push(`/library/playlist/${playlist.id}`);
  }
  const { pressableProps } = usePressableInteraction(sidebarItemStyle(theme, 'idle'));
  return (
    <Tappable {...sidebarPlaylistRowProps(playlist, onPress)} {...pressableProps}>
      <SidebarPlaylistLabel name={playlist.name} color={theme.color.textSecondary} />
    </Tappable>
  );
}

function SidebarPlaylistsEmptyHint() {
  const theme = useTheme();
  return (
    <Text
      testID="sidebar-playlists-empty"
      variant="caption"
      style={{ color: theme.color.textTertiary }}
    >
      No playlists yet
    </Text>
  );
}

function SidebarPlaylistsLoadingHint() {
  const theme = useTheme();
  return (
    <Text
      testID="sidebar-playlists-loading"
      variant="caption"
      style={{ color: theme.color.textTertiary }}
    >
      Loading…
    </Text>
  );
}

function sidebarPlaylistsRetryProps(onRetry: () => void, theme: Theme) {
  return {
    action: 'sidebar.playlists.retry',
    testID: 'sidebar-playlists-retry',
    onPress: onRetry,
    accessibilityRole: 'button' as const,
    accessibilityLabel: 'Retry loading playlists',
    style: sidebarItemStyle(theme, 'idle'),
  };
}

function SidebarPlaylistsRetryButton({ onRetry, theme }: { onRetry: () => void; theme: Theme }) {
  return (
    <Tappable {...sidebarPlaylistsRetryProps(onRetry, theme)}>
      <Text variant="caption" style={{ color: theme.color.accent }}>
        Retry
      </Text>
    </Tappable>
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

type RowsProps = { playlists: PlaylistResponse[]; navigator: Navigator };

function SidebarPlaylistRows({ playlists, navigator }: RowsProps) {
  return (
    <View>
      {playlists.map((playlist) => (
        <SidebarPlaylistRow key={playlist.id} playlist={playlist} navigator={navigator} />
      ))}
    </View>
  );
}

function sidebarPlaylistsView(
  playlists: PlaylistResponse[],
  isLoading: boolean,
  error: Error | null,
) {
  return asyncView({ isLoading, isError: error != null, isEmpty: playlists.length === 0 });
}

type SidebarPlaylistsProps = { navigator: Navigator };

export function SidebarPlaylists({ navigator }: SidebarPlaylistsProps) {
  const { playlists, playlistsError, isLoadingPlaylists, refetchPlaylists } = usePlaylistActions();
  const view = sidebarPlaylistsView(playlists, isLoadingPlaylists, playlistsError);

  if (view === 'loading') return <SidebarPlaylistsLoadingHint />;
  if (view === 'error') return <SidebarPlaylistsErrorHint onRetry={refetchPlaylists} />;
  if (view === 'empty') return <SidebarPlaylistsEmptyHint />;
  return <SidebarPlaylistRows playlists={playlists} navigator={navigator} />;
}
