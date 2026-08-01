import type { ReactElement, ReactNode } from 'react';
import { Alert } from 'react-native';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import * as api from '@shared/api-client/playlists';
import { playlistKeys } from '@shared/lib/query-keys';

import {
  useAddTracksToPlaylist,
  useCreatePlaylistWithTracks,
  useRemoveTracksFromPlaylist,
} from '../mutations';

jest.mock('@shared/api-client/playlists');

const addTracksToPlaylist = api.addTracksToPlaylist as jest.MockedFunction<
  typeof api.addTracksToPlaylist
>;
const removeTracksFromPlaylist = api.removeTracksFromPlaylist as jest.MockedFunction<
  typeof api.removeTracksFromPlaylist
>;
const createPlaylist = api.createPlaylist as jest.MockedFunction<typeof api.createPlaylist>;

const clients: QueryClient[] = [];

function makeClient(): QueryClient {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  return client;
}

afterEach(() => {
  while (clients.length > 0) {
    clients.pop()?.clear();
  }
});

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }): ReactElement {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function seedList(client: QueryClient): void {
  client.setQueryData(playlistKeys.list, {
    items: [
      { id: 'p1', name: 'Focus', track_count: 3 },
      { id: 'p2', name: 'Other', track_count: 9 },
    ],
    total: 2,
  });
}

function listCount(client: QueryClient, playlistId: string): number | undefined {
  return client
    .getQueryData<{ items: { id: string; track_count: number }[] }>(playlistKeys.list)
    ?.items.find((p) => p.id === playlistId)?.track_count;
}

beforeEach(() => {
  jest.clearAllMocks();
  jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
});

describe('useAddTracksToPlaylist', () => {
  it('sends the whole selection in one request, not one request per track', async () => {
    const client = makeClient();
    addTracksToPlaylist.mockResolvedValue({ added: 3, skipped: 0 });
    const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ playlistId: 'p1', trackIds: ['t1', 't2', 't3'] }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(addTracksToPlaylist).toHaveBeenCalledTimes(1);
    expect(addTracksToPlaylist).toHaveBeenCalledWith('p1', { track_ids: ['t1', 't2', 't3'] });
  });

  it('bumps the cached track_count by the size of the selection before the server answers', async () => {
    const client = makeClient();
    seedList(client);
    let release: (v: { added: number; skipped: number }) => void = () => {};
    addTracksToPlaylist.mockReturnValue(
      new Promise((resolve) => {
        release = resolve;
      }),
    );
    const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ playlistId: 'p1', trackIds: ['t1', 't2'] }));

    await waitFor(() => expect(listCount(client, 'p1')).toBe(5));
    expect(listCount(client, 'p2')).toBe(9);
    act(() => release({ added: 2, skipped: 0 }));
  });

  it('rolls the optimistic count back when the add fails', async () => {
    const client = makeClient();
    seedList(client);
    addTracksToPlaylist.mockRejectedValue(new Error('boom'));
    const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ playlistId: 'p1', trackIds: ['t1', 't2'] }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(listCount(client, 'p1')).toBe(3);
    expect(Alert.alert).toHaveBeenCalledWith('Add failed', expect.stringContaining('tracks'));
  });

  it('reports the tracks the server skipped rather than claiming every one landed', async () => {
    const client = makeClient();
    seedList(client);
    addTracksToPlaylist.mockResolvedValue({ added: 1, skipped: 2 });
    const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ playlistId: 'p1', trackIds: ['t1', 't2', 't3'] }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(Alert.alert).toHaveBeenCalledWith('Note', '2 tracks were already in Focus.');
  });

  it('stays quiet when every requested track was added', async () => {
    const client = makeClient();
    seedList(client);
    addTracksToPlaylist.mockResolvedValue({ added: 2, skipped: 0 });
    const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ playlistId: 'p1', trackIds: ['t1', 't2'] }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(Alert.alert).not.toHaveBeenCalled();
  });
});

describe('useCreatePlaylistWithTracks', () => {
  it('creates the playlist, then adds the whole selection to it', async () => {
    const client = makeClient();
    createPlaylist.mockResolvedValue({
      id: 'new',
      name: 'Road trip',
      track_count: 0,
      preview_artwork_urls: [],
      created_at: '',
      updated_at: '',
    });
    addTracksToPlaylist.mockResolvedValue({ added: 2, skipped: 0 });
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ name: 'Road trip', trackIds: ['t1', 't2'] }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(createPlaylist).toHaveBeenCalledWith({ name: 'Road trip' });
    expect(addTracksToPlaylist).toHaveBeenCalledWith('new', { track_ids: ['t1', 't2'] });
  });

  it('keeps the created playlist and tells the user when only the add failed', async () => {
    const client = makeClient();
    createPlaylist.mockResolvedValue({
      id: 'new',
      name: 'Road trip',
      track_count: 0,
      preview_artwork_urls: [],
      created_at: '',
      updated_at: '',
    });
    addTracksToPlaylist.mockRejectedValue(new Error('boom'));
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), { wrapper: wrapper(client) });

    act(() => result.current.mutate({ name: 'Road trip', trackIds: ['t1', 't2'] }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.playlist.id).toBe('new');
    expect(Alert.alert).toHaveBeenCalledWith('Note', expect.stringContaining('Playlist created'));
  });
});

describe('useRemoveTracksFromPlaylist', () => {
  it('drops every selected track from the cached detail at once', async () => {
    const client = makeClient();
    client.setQueryData(playlistKeys.detail('p1'), {
      id: 'p1',
      name: 'Focus',
      tracks: [{ id: 't1' }, { id: 't2' }, { id: 't3' }],
    });
    let release: (v: { removed: number }) => void = () => {};
    removeTracksFromPlaylist.mockReturnValue(
      new Promise((resolve) => {
        release = resolve;
      }),
    );
    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: wrapper(client),
    });

    act(() => result.current.mutate(['t1', 't3']));

    await waitFor(() =>
      expect(
        client.getQueryData<{ tracks: { id: string }[] }>(playlistKeys.detail('p1'))?.tracks,
      ).toEqual([{ id: 't2' }]),
    );
    expect(removeTracksFromPlaylist).toHaveBeenCalledTimes(1);
    act(() => release({ removed: 2 }));
  });

  it('restores every removed track when the request fails', async () => {
    const client = makeClient();
    const original = [{ id: 't1' }, { id: 't2' }, { id: 't3' }];
    client.setQueryData(playlistKeys.detail('p1'), { id: 'p1', name: 'Focus', tracks: original });
    removeTracksFromPlaylist.mockRejectedValue(new Error('boom'));
    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: wrapper(client),
    });

    act(() => result.current.mutate(['t1', 't3']));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(
      client.getQueryData<{ tracks: { id: string }[] }>(playlistKeys.detail('p1'))?.tracks,
    ).toEqual(original);
  });
});
