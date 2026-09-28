import Head from 'expo-router/head';
import type { ReactElement } from 'react';

import { usePlaylistActions } from '../features/library/hooks/usePlaylistActions';
import { usePlayback } from '../shared/playback/usePlayback';
import { useKeyboardShortcuts } from '../shared/ui/keyboard/useKeyboardShortcuts';
import { pageTitleFor } from '../shared/ui/navigation/pageTitle';

const PLAYLIST_PATH_PREFIX = '/library/playlist/';

export function SystemNavigationBar(_props: { scheme: 'light' | 'dark' }): ReactElement | null {
  return null;
}

export function PlaybackShortcuts(): null {
  const playback = usePlayback();
  useKeyboardShortcuts(playback);
  return null;
}

function usePlaylistNameFromPath(pathname: string): string | undefined {
  const { playlists } = usePlaylistActions();
  const [, playlistId] = pathname.match(/^\/library\/playlist\/([^/]+)/) ?? [];
  return playlistId ? playlists.find((playlist) => playlist.id === playlistId)?.name : undefined;
}

function PlaylistTabsTitle({ pathname }: { pathname: string }) {
  const playlistName = usePlaylistNameFromPath(pathname);
  return (
    <Head>
      <title>{pageTitleFor(pathname, playlistName)}</title>
    </Head>
  );
}

export function TabsTitle({ pathname }: { pathname: string }): ReactElement | null {
  if (pathname.startsWith(PLAYLIST_PATH_PREFIX)) return <PlaylistTabsTitle pathname={pathname} />;
  return (
    <Head>
      <title>{pageTitleFor(pathname)}</title>
    </Head>
  );
}
