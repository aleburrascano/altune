import Head from 'expo-router/head';
import type { ReactElement } from 'react';

import { usePlaylistActions } from '@features/library';
import { usePlayback } from '@shared/playback';
import { useKeyboardShortcuts, pageTitleFor } from '@shared/ui';
import { tabsTitleFor } from '@/mobile/frame/tabsTitle';

const PLAYLIST_PATH_PREFIX = '/library/playlist/';

export function SystemNavigationBar(_props: { scheme: 'light' | 'dark' }): ReactElement | null {
  return null;
}

export function PlaybackShortcuts(): null {
  const playback = usePlayback();
  useKeyboardShortcuts(playback, window);
  return null;
}

function PlaylistTabsTitle({ pathname }: { pathname: string }) {
  const { playlists } = usePlaylistActions();
  return (
    <Head>
      <title>{tabsTitleFor(pathname, playlists)}</title>
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
