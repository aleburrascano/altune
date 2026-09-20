import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { useCreatePlaylist, useCreatePlaylistWithTracks } from '../mutations';
import { ContractError } from '@shared/errors';
import { asTrackId } from '@shared/api-client/ids';
import { playlistKeys } from '@shared/lib/query-keys';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

// A full PlaylistResponse: the api-client now contract-parses the create body,
// so a thin { id, name } payload would be rejected before the hook sees it.
function created(id: string, name: string) {
  return {
    id,
    name,
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

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
  it('a dropped connection between the two awaited requests deletes the playlist the create already landed, leaving no empty orphan behind', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
    });

    expect(__http.countFor('DELETE /v1/playlists/p1')).toBe(1);
  });

  it('a rolled-back create resolves the mutation without a dangling playlist, so no caller can act on one that no longer exists', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
    await act(async () => {
      data = await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
    });

    expect(result.current.isError).toBe(false);
    expect(data).toEqual({ added: 0, addFailed: true });
  });

  it('a transient 5xx from the batch-add endpoint is compensated the same way, not rethrown as a mutation error', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p2', 'Chill') });
    __http.reply('POST /v1/playlists/p2/tracks/batch', { status: 503 });
    __http.reply('DELETE /v1/playlists/p2', { status: 204 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
    await act(async () => {
      data = await result.current.mutateAsync({
        name: 'Chill',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });

    expect(result.current.isError).toBe(false);
    expect(data.addFailed).toBe(true);
    expect(data.playlist).toBeUndefined();
    expect(__http.countFor('DELETE /v1/playlists/p2')).toBe(1);
  });

  it('an add that lands never triggers the compensating delete', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
    });

    expect(__http.countFor('DELETE /v1/playlists/p1')).toBe(0);
  });

  it('the compensating delete failing too keeps the created playlist in the resolved result, the only case a caller is handed one', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    __http.fail('DELETE /v1/playlists/p1');
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
    await act(async () => {
      data = await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
    });

    expect(result.current.isError).toBe(false);
    expect(data).toEqual({ playlist: created('p1', 'Focus'), added: 0, addFailed: true });
  });
});

describe('useCreatePlaylistWithTracks: onSuccess note after a failed add (:45-54)', () => {
  it('a rolled-back create tells the user nothing was created, never that a playlist is waiting for manual adds', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: 'Focus',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });

    expect(alertSpy).toHaveBeenCalledTimes(1);
    expect(alertSpy).toHaveBeenCalledWith(
      'Error',
      'Could not create the playlist. Please try again.',
    );
  });

  it.each([
    [
      'a single requested track',
      [asTrackId('t1')],
      'Playlist created, but the track could not be added. Try adding it manually.',
    ],
    [
      'more than one requested track',
      [asTrackId('t1'), asTrackId('t2')],
      'Playlist created, but the tracks could not be added. Try adding them manually.',
    ],
  ] as const)(
    '%s -> a surviving orphan keeps the manual-add copy, singular/plural on trackIds.length === 1',
    async (_label, trackIds, expectedMessage) => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.fail('DELETE /v1/playlists/p1');
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({ name: 'Focus', trackIds: [...trackIds] });
      });

      expect(alertSpy).toHaveBeenCalledWith('Note', expectedMessage);
    },
  );

  it('a surviving orphan suppresses the skip-count alert even though added(0) < trackIds.length', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    __http.fail('DELETE /v1/playlists/p1');
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: 'Focus',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });

    expect(alertSpy).toHaveBeenCalledTimes(1);
  });
});

describe('useCreatePlaylistWithTracks: onSuccess skip-count note on a brand-new playlist (:55-57), alreadyThereMessage table', () => {
  it.each([
    [
      'a duplicate track id inside one request, playlist name known from the create response',
      [asTrackId('t1'), asTrackId('t1')],
      { added: 1, skipped: 1 },
      'Focus',
      'One track was already in Focus.',
    ],
    [
      'a track deleted between building the selection and the request landing, playlist name known',
      [asTrackId('t1'), asTrackId('t2'), asTrackId('t3')],
      { added: 1, skipped: 2 },
      'Focus',
      '2 tracks were already in Focus.',
    ],
  ] as const)('%s', async (_label, trackIds, addResponse, playlistName, expectedMessage) => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', playlistName) });
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

  it('a thin create response missing name is rejected by the contract layer: a mutation error, never a batch-add', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1' } });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    let caught: unknown;
    await act(async () => {
      try {
        await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      } catch (error) {
        caught = error;
      }
    });

    expect(caught).toBeInstanceOf(ContractError);
    expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(0);
    expect(alertSpy).toHaveBeenCalledWith(
      'Error',
      'Could not create the playlist. Please try again.',
    );
  });

  it('every requested track lands (added === trackIds.length) stays silent, no false skip note', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: 'Focus',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });

    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('a batch body missing added is refused by the contract layer, so the skip note can never read "NaN tracks"', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { skipped: 0 } });
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: 'Focus',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });

    expect(alertSpy).not.toHaveBeenCalledWith('Note', expect.stringContaining('NaN'));
    expect(alertSpy).toHaveBeenCalledWith(
      'Error',
      'Could not create the playlist. Please try again.',
    );
  });

  it('a malformed server body claiming more added than requested does not produce a lying skip-count note', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 5, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: 'Focus',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
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
    expect(alertSpy).toHaveBeenCalledWith(
      'Error',
      'Could not create the playlist. Please try again.',
    );
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
        await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      } catch (error) {
        caught = error;
      }
    });

    expect(caught).toBeDefined();
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(alertSpy).toHaveBeenCalledWith(
      'Error',
      'Could not create the playlist. Please try again.',
    );
    expect(
      __http.requests.filter((r: { path: string }) => r.path.includes('tracks/batch')),
    ).toHaveLength(0);
  });
});

describe('onSettled invalidation (:29, :62) — exact key identity', () => {
  it('useCreatePlaylist invalidates playlistKeys.list, and only that key, on success', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
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

  it('useCreatePlaylistWithTracks invalidates playlistKeys.list, and only that key, even when the add step failed and the playlist was rolled back', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = freshClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
    });

    expect(invalidateSpy).toHaveBeenCalledTimes(1);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });
});

describe('request bodies sent over the wire', () => {
  it('useCreatePlaylist POSTs the exact { name } body, not an empty one', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Road Trip Mix') });
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
    __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Road Trip Mix') });
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = freshClient();
    const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: 'Road Trip Mix',
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
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
