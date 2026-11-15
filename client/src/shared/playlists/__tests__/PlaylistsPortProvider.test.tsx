import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import {
  httpPlaylistsPort,
  createMemoryPlaylistsPort,
  PlaylistsPortProvider,
  usePlaylistsPort,
} from '@shared/playlists';
describe('usePlaylistsPort', () => {
  it('hands back the HTTP port when no provider is mounted', () => {
    const { result } = renderHook(() => usePlaylistsPort());

    expect(result.current).toBe(httpPlaylistsPort);
  });

  it('hands back the port the provider was given', () => {
    const port = createMemoryPlaylistsPort();
    function wrapper({ children }: { children: ReactNode }) {
      return <PlaylistsPortProvider port={port}>{children}</PlaylistsPortProvider>;
    }

    const { result } = renderHook(() => usePlaylistsPort(), { wrapper });

    expect(result.current).toBe(port);
  });
});
