import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { useCreatePlaylist, useCreatePlaylistWithTracks } from '../mutations';
import { playlistKeys } from '@shared/lib/query-keys';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function freshClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

let alertSpy: jest.SpyInstance;

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('useCreatePlaylistWithTracks: addTracksToPlaylist fails after createPlaylist already landed (:36-43)', () => {
  it('a dropped connection between the two awaited requests still resolves the mutation, carrying the created playlist with addFailed true', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
    await act(async () => {
      data = await result.current.mutateAsync({ name: 'Focus', trackIds: ['t1'] });
    });

    expect(result.current.isError).toBe(false);
    expect(data).toEqual({ playlist: { id: 'p1', name: 'Focus' }, added: 0, addFailed: true });
  });

  it('a transient 5xx from the batch-add endpoint is swallowed the same way, not rethrown as a mutation error', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p2', name: 'Chill' } });
    __http.reply('POST /v1/playlists/p2/tracks/batch', { status: 503 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
    await act(async () => {
      data = await result.current.mutateAsync({ name: 'Chill', trackIds: ['t1', 't2'] });
    });

    expect(result.current.isError).toBe(false);
    expect(data.addFailed).toBe(true);
    expect(data.playlist).toEqual({ id: 'p2', name: 'Chill' });
  });
});

describe('useCreatePlaylistWithTracks: onSuccess add-failed note (:45-54)', () => {
  it.each([
    ['a single requested track', ['t1'], 'Playlist created, but the track could not be added. Try adding it manually.'],
    [
      'more than one requested track',
      ['t1', 't2'],
      'Playlist created, but the tracks could not be added. Try adding them manually.',
    ],
  ] as const)('%s -> the singular/plural copy branch on trackIds.length === 1', async (_label, trackIds, expectedMessage) => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: [...trackIds] });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', expectedMessage);
  });

  it('the early return suppresses the skip-count alert even though added(0) < trackIds.length', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: ['t1', 't2'] });
    });

    expect(alertSpy).toHaveBeenCalledTimes(1);
  });
});

describe('useCreatePlaylistWithTracks: onSuccess skip-count note on a brand-new playlist (:55-57), alreadyThereMessage table', () => {
  it.each([
    [
      'a duplicate track id inside one request, playlist name known from the create response',
      ['t1', 't1'],
      { added: 1, skipped: 1 },
      'Focus',
      'One track was already in Focus.',
    ],
    [
      'a track deleted between building the selection and the request landing, playlist name known',
      ['t1', 't2', 't3'],
      { added: 1, skipped: 2 },
      'Focus',
      '2 tracks were already in Focus.',
    ],
    [
      'skipped count of one, but the create response is a thin payload missing name',
      ['t1', 't2'],
      { added: 1, skipped: 1 },
      undefined,
      'One track was already in the playlist.',
    ],
    [
      'skipped count of two, but the create response is a thin payload missing name',
      ['t1', 't2', 't3'],
      { added: 1, skipped: 2 },
      undefined,
      '2 tracks were already in the playlist.',
    ],
  ] as const)('%s', async (_label, trackIds, addResponse, playlistName, expectedMessage) => {
    const createJson: Record<string, unknown> = { id: 'p1' };
    if (playlistName !== undefined) createJson.name = playlistName;
    __http.reply('POST /v1/playlists', { status: 201, json: createJson });
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: addResponse });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'irrelevant', trackIds: [...trackIds] });
    });

    expect(result.current.isError).toBe(false);
    expect(alertSpy).toHaveBeenCalledWith('Note', expectedMessage);
  });

  it('every requested track lands (added === trackIds.length) stays silent, no false skip note', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: ['t1', 't2'] });
    });

    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('a malformed server body claiming more added than requested does not produce a lying skip-count note', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 5, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: ['t1', 't2'] });
    });

    expect(alertSpy).not.toHaveBeenCalled();
  });
});

describe('createPlaylist itself failing (:26-28, :59-61)', () => {
  it('useCreatePlaylist surfaces a server 500 as a mutation error and alerts the user, without creating anything', async () => {
    __http.reply('POST /v1/playlists', { status: 500 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    let caught: unknown;
    await act(async () => {
      try {
        await result.current.mutateAsync('Focus');
      } catch (error) {
        caught = error;
      }
    });

    expect(caught).toBeDefined();
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(alertSpy).toHaveBeenCalledWith('Error', 'Could not create the playlist. Please try again.');
  });

  it('useCreatePlaylistWithTracks surfaces the same failure, and never reaches the batch-add endpoint', async () => {
    __http.reply('POST /v1/playlists', { status: 500 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let caught: unknown;
    await act(async () => {
      try {
        await result.current.mutateAsync({ name: 'Focus', trackIds: ['t1'] });
      } catch (error) {
        caught = error;
      }
    });

    expect(caught).toBeDefined();
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(alertSpy).toHaveBeenCalledWith('Error', 'Could not create the playlist. Please try again.');
    expect(__http.requests.filter((r: { path: string }) => r.path.includes('tracks/batch'))).toHaveLength(0);
  });
});

describe('onSettled invalidation (:29, :62) — exact key identity', () => {
  it('useCreatePlaylist invalidates playlistKeys.list, and only that key, on success', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    const queryClient = freshClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    const { result } = renderHook(() => useCreatePlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('Focus');
    });

    expect(invalidateSpy).toHaveBeenCalledTimes(1);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });

  it('useCreatePlaylist invalidates playlistKeys.list even when the create call fails', async () => {
    __http.reply('POST /v1/playlists', { status: 500 });
    const queryClient = freshClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    const { result } = renderHook(() => useCreatePlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('Focus').catch(() => {});
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });

  it('useCreatePlaylistWithTracks invalidates playlistKeys.list, and only that key, even when the add step failed', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = freshClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: ['t1'] });
    });

    expect(invalidateSpy).toHaveBeenCalledTimes(1);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });
});

describe('request bodies sent over the wire', () => {
  it('useCreatePlaylist POSTs the exact { name } body, not an empty one', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Road Trip Mix' } });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('Road Trip Mix');
    });

    const request = __http.requests.find(
      (r: { method: string; path: string }) => r.method === 'POST' && r.path === '/v1/playlists',
    );
    expect(JSON.parse(request.body)).toEqual({ name: 'Road Trip Mix' });
  });

  it('useCreatePlaylistWithTracks POSTs the exact { name } create body and the exact { track_ids } add body, neither an empty one', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Road Trip Mix' } });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Road Trip Mix', trackIds: ['t1', 't2'] });
    });

    const createRequest = __http.requests.find(
      (r: { method: string; path: string }) => r.method === 'POST' && r.path === '/v1/playlists',
    );
    const addRequest = __http.requests.find(
      (r: { method: string; path: string }) =>
        r.method === 'POST' && r.path === '/v1/playlists/p1/tracks/batch',
    );
    expect(JSON.parse(createRequest.body)).toEqual({ name: 'Road Trip Mix' });
    expect(JSON.parse(addRequest.body)).toEqual({ track_ids: ['t1', 't2'] });
  });
});
