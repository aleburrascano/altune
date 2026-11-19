import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { PlaylistsPortProvider, usePlaylistsPort } from '@shared/playlists';
import { createMemoryPlaylistsPort } from '@testing/fakes/playlists/inMemoryPlaylistsPort';
describe('usePlaylistsPort', () => {
  it('throws when no provider is mounted', () => {
    expect(() => renderHook(() => usePlaylistsPort())).toThrow('no playlists port provided');
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
