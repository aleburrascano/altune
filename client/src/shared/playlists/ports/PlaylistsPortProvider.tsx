import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpPlaylistsPort } from '@shared/playlists/adapters/http/httpPlaylistsPort';
import type { PlaylistsPort } from '@shared/playlists/ports/playlistsPort';

const PlaylistsPortContext = createContext<PlaylistsPort>(httpPlaylistsPort);

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
  return use(PlaylistsPortContext);
}
