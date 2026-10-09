import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asPlaylistId, NO_PLAYLIST_ID } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { playlistKeys } from '@shared/query/queryKeys';

import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';

import { usePlaylistDetail } from '../hooks/usePlaylistDetail';

const detail: PlaylistDetailResponse = {
  id: asPlaylistId('p1'),
  name: 'Mix',
  track_count: 0,
  preview_artwork_urls: [],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  total_duration_seconds: 0,
  tracks: [],
};

let client: QueryClient;
let port: LibraryPort & { getPlaylist: jest.Mock };

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const memory = createMemoryLibraryPort({ playlists: [detail] });
  port = { ...memory, getPlaylist: jest.fn(memory.getPlaylist) };
});

afterEach(() => client.clear());

describe('usePlaylistDetail', () => {
  it('fetches the playlist and caches it under the detail key', async () => {
    const id = asPlaylistId('p1');

    const { result } = renderHook(() => usePlaylistDetail(id), { wrapper });

    await waitFor(() => expect(result.current.data).toEqual(detail));
    expect(port.getPlaylist).toHaveBeenCalledWith('p1');
    expect(client.getQueryData(playlistKeys.detail(id))).toEqual(detail);
  });

  it('does not fetch when the id is empty', () => {
    const { result } = renderHook(() => usePlaylistDetail(NO_PLAYLIST_ID), { wrapper });

    expect(port.getPlaylist).not.toHaveBeenCalled();
    expect(result.current.isLoading).toBe(false);
  });

  it('surfaces fetch errors', async () => {
    port.getPlaylist.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => usePlaylistDetail(asPlaylistId('p1')), { wrapper });

    await waitFor(() => expect(result.current.error).toEqual(new Error('boom')));
  });

  it('never goes stale on its own', async () => {
    const { result } = renderHook(() => usePlaylistDetail(asPlaylistId('p1')), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.isStale).toBe(false);
  });
});
