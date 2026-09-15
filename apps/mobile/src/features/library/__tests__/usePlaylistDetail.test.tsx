// #782: the playlist detail query lives behind a hook. It must keep the same query key,
// fetch, and skip-on-empty-id behavior the screen had inline.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asPlaylistId, NO_PLAYLIST_ID, type PlaylistId } from '@shared/api-client/ids';
import { playlistKeys } from '@shared/lib/query-keys';

import { usePlaylistDetail } from '../hooks/usePlaylistDetail';

const mockGetPlaylist = jest.fn();
jest.mock('@shared/api-client/playlists', () => ({
  getPlaylist: (id: PlaylistId) => mockGetPlaylist(id),
}));

let client: QueryClient;

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockGetPlaylist.mockReset();
});

describe('usePlaylistDetail', () => {
  it('fetches the playlist and caches it under the detail key', async () => {
    const detail = { id: 'p1', name: 'Mix', tracks: [] };
    mockGetPlaylist.mockResolvedValue(detail);
    const id = asPlaylistId('p1');

    const { result } = renderHook(() => usePlaylistDetail(id), { wrapper });

    await waitFor(() => expect(result.current.data).toEqual(detail));
    expect(mockGetPlaylist).toHaveBeenCalledWith('p1');
    expect(client.getQueryData(playlistKeys.detail(id))).toEqual(detail);
  });

  it('does not fetch when the id is empty', () => {
    const { result } = renderHook(() => usePlaylistDetail(NO_PLAYLIST_ID), { wrapper });

    expect(mockGetPlaylist).not.toHaveBeenCalled();
    expect(result.current.isLoading).toBe(false);
  });

  it('surfaces fetch errors', async () => {
    mockGetPlaylist.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() => usePlaylistDetail(asPlaylistId('p1')), { wrapper });

    await waitFor(() => expect(result.current.error).toEqual(new Error('boom')));
  });

  it('never goes stale on its own', async () => {
    mockGetPlaylist.mockResolvedValue({ id: 'p1', name: 'Mix', tracks: [] });

    const { result } = renderHook(() => usePlaylistDetail(asPlaylistId('p1')), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.isStale).toBe(false);
  });
});
