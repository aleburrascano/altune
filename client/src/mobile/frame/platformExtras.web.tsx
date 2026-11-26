import Head from 'expo-router/head';
import type { ReactElement } from 'react';

import { usePlaylistActions } from '@features/library';
import { usePlayback } from '@shared/playback';
import { useKeyboardShortcuts, pageTitleFor } from '@shared/ui';
const PLAYLIST_PATH_PREFIX = '/library/playlist/';
const PLAYLIST_PATH_PATTERN = /^\/library\/playlist\/([^/]+)/;

export function SystemNavigationBar(_props: { scheme: 'light' | 'dark' }): ReactElement | null {
  return null;
}

export function PlaybackShortcuts(): null {
  const playback = usePlayback();
  useKeyboardShortcuts(playback, window);
  return null;
}

function usePlaylistNameFromPath(pathname: string): string | undefined {
  const { playlists } = usePlaylistActions();
  const playlistId = PLAYLIST_PATH_PATTERN.exec(pathname)?.[1];
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
