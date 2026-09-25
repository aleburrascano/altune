import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { asPlaylistId, asTrackId, type PlaylistId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import {
  useAddTracksToPlaylist,
  useCreatePlaylist,
  useCreatePlaylistWithTracks,
  useDeletePlaylist,
} from '../mutations';

const mockCreatePlaylist = jest.fn<Promise<PlaylistResponse>, [{ name: string }]>();
const mockAddTracks = jest.fn<Promise<{ added: number }>, [PlaylistId, unknown]>();
const mockDeletePlaylist = jest.fn<Promise<void>, [PlaylistId]>();
jest.mock('@shared/api-client/playlists', () => ({
  createPlaylist: (body: { name: string }) => mockCreatePlaylist(body),
  addTracksToPlaylist: (id: PlaylistId, body: unknown) => mockAddTracks(id, body),
  deletePlaylist: (id: PlaylistId) => mockDeletePlaylist(id),
  removeTracksFromPlaylist: jest.fn(),
  renamePlaylist: jest.fn(),
}));

type Deferred<T> = { promise: Promise<T>; resolve: (v: T) => void; reject: (e: Error) => void };

function deferred<T>(): Deferred<T> {
  let resolve!: (v: T) => void;
  let reject!: (e: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function playlist(id: string, name: string, trackCount = 0): PlaylistResponse {
  return {
    id: asPlaylistId(id),
    name,
    track_count: trackCount,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  } as PlaylistResponse;
}

const USER_B_PLAYLISTS = { items: [playlist('b1', 'B Mix', 3)] };

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }
  const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
  return { queryClient, wrapper: Wrapper, invalidateSpy };
}

function signOutThenLoadUserB(queryClient: QueryClient): void {
  runSignOutCleanups();
  queryClient.clear();
  queryClient.setQueryData(playlistKeys.list, USER_B_PLAYLISTS);
}

let alertSpy: jest.SpyInstance;

beforeEach(() => {
  mockCreatePlaylist.mockReset();
  mockAddTracks.mockReset();
  mockDeletePlaylist.mockReset();
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('playlist mutations that settle after sign-out leave the next user untouched (#2729)', () => {
  it('useCreatePlaylist: a create failing after sign-out neither alerts nor refetches user B playlists', async () => {
    const { queryClient, wrapper, invalidateSpy } = setup();
    const request = deferred<PlaylistResponse>();
    mockCreatePlaylist.mockReturnValue(request.promise);
    const { result } = renderHook(() => useCreatePlaylist(), { wrapper });
    act(() => result.current.mutate('A Mix'));
    await waitFor(() => expect(mockCreatePlaylist).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient);
    await act(async () => request.reject(new Error('offline')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(alertSpy).not.toHaveBeenCalled();
    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(USER_B_PLAYLISTS);
  });

  it("useCreatePlaylistWithTracks: a create landing after sign-out never sends the add under the next user's session", async () => {
    const { queryClient, wrapper } = setup();
    const create = deferred<PlaylistResponse>();
    mockCreatePlaylist.mockReturnValue(create.promise);
    mockAddTracks.mockResolvedValue({ added: 1 });
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), { wrapper });
    act(() => result.current.mutate({ name: 'A Mix', trackIds: [asTrackId('a1')] }));
    await waitFor(() => expect(mockCreatePlaylist).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient);
    await act(async () => create.resolve(playlist('a-new', 'A Mix')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(mockAddTracks).not.toHaveBeenCalled();
    expect(mockDeletePlaylist).not.toHaveBeenCalled();
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it("useCreatePlaylistWithTracks: an add failing after sign-out does not roll back under the next user's session", async () => {
    const { queryClient, wrapper } = setup();
    mockCreatePlaylist.mockResolvedValue(playlist('a-new', 'A Mix'));
    const add = deferred<{ added: number }>();
    mockAddTracks.mockReturnValue(add.promise);
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), { wrapper });
    act(() => result.current.mutate({ name: 'A Mix', trackIds: [asTrackId('a1')] }));
    await waitFor(() => expect(mockAddTracks).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient);
    await act(async () => add.reject(new Error('offline')));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(mockDeletePlaylist).not.toHaveBeenCalled();
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('useDeletePlaylist: a delete failing after sign-out does not alert the next user', async () => {
    const { queryClient, wrapper } = setup();
    const request = deferred<void>();
    mockDeletePlaylist.mockReturnValue(request.promise);
    const { result } = renderHook(() => useDeletePlaylist(asPlaylistId('a1')), { wrapper });
    act(() => result.current.mutate());
    await waitFor(() => expect(mockDeletePlaylist).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient);
    await act(async () => request.reject(new Error('offline')));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('useDeletePlaylist: a delete succeeding after sign-out does not refetch user B playlists', async () => {
    const { queryClient, wrapper, invalidateSpy } = setup();
    const request = deferred<void>();
    mockDeletePlaylist.mockReturnValue(request.promise);
    const { result } = renderHook(() => useDeletePlaylist(asPlaylistId('a1')), { wrapper });
    act(() => result.current.mutate());
    await waitFor(() => expect(mockDeletePlaylist).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient);
    await act(async () => request.resolve());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("useAddTracksToPlaylist: an add answering after sign-out does not tell user B about user A's playlist", async () => {
    const { queryClient, wrapper } = setup();
    queryClient.setQueryData(playlistKeys.list, { items: [playlist('a1', 'A Mix', 2)] });
    const request = deferred<{ added: number }>();
    mockAddTracks.mockReturnValue(request.promise);
    const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper });
    const trackIds = [asTrackId('t1'), asTrackId('t2')];
    act(() => result.current.mutate({ playlistId: asPlaylistId('a1'), trackIds }));
    await waitFor(() => expect(mockAddTracks).toHaveBeenCalled());

    signOutThenLoadUserB(queryClient);
    await act(async () => request.resolve({ added: 0 }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(alertSpy).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(USER_B_PLAYLISTS);
  });
});
