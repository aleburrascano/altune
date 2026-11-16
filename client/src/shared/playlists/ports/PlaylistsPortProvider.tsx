import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { PlaylistsPort } from '@shared/playlists/ports/playlistsPort';

const PlaylistsPortContext = createContext<PlaylistsPort | null>(null);

interface PlaylistsPortProviderProps {
  port: PlaylistsPort;
  children: ReactNode;
}

export function PlaylistsPortProvider({
  port,
  children,
}: PlaylistsPortProviderProps): ReactElement {
  return <PlaylistsPortContext.Provider value={port}>{children}</PlaylistsPortContext.Provider>;
}

export function usePlaylistsPort(): PlaylistsPort {
  const port = use(PlaylistsPortContext);
  if (port === null) throw new Error('no playlists port provided: mount PlaylistsPortProvider');
  return port;
}
