import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { httpPlaylistsPort } from '@shared/playlists/adapters/http/httpPlaylistsPort';
import { createMemoryPlaylistsPort } from '@shared/playlists/__tests__/memoryPlaylistsPort';
import {
  PlaylistsPortProvider,
  usePlaylistsPort,
} from '@shared/playlists/ports/PlaylistsPortProvider';

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
