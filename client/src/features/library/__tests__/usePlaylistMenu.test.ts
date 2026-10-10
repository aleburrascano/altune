import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';
import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { Navigator } from '@shared/navigation';
import { createMemoryNavigator } from '@shared/navigation/__tests__/memoryNavigator';
import {
  usePlaylistDelete,
  usePlaylistOfflineAction,
} from '@features/library/hooks/usePlaylistMenu';
import { Alert, type AlertButton } from 'react-native';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { supabase } from '@shared/auth/supabaseClient';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { playlistKeys } from '@shared/query/queryKeys';
import { trackResponseFactory } from '@testing/factories';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const DELETE_ROUTE = 'DELETE /v1/playlists/pl1';
const TELEMETRY_ROUTE = 'POST /v1/discovery/events';
const HTTP_NO_CONTENT = 204;
const HTTP_ACCEPTED = 202;
const TITLE_AND_MESSAGE = 2;

type MemoryNavigator = ReturnType<typeof createMemoryNavigator>;

interface WrapperProps {
  children: ReactNode;
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  __http.reply(TELEMETRY_ROUTE, { status: HTTP_ACCEPTED });
});

describe('usePlaylistDelete', () => {
  const PLAYLIST_ID = asPlaylistId('pl1');
  let queryClient: QueryClient;
  let alertSpy: jest.SpyInstance;

  function wrapper({ children }: WrapperProps) {
    return createElement(QueryClientProvider, { client: queryClient }, children);
  }

  function openDeletePrompt(navigator: Navigator): void {
    const { result } = renderHook(() => usePlaylistDelete(PLAYLIST_ID, navigator), { wrapper });
    act(() => result.current());
  }

  function promptButtons(): AlertButton[] {
    const buttons = alertSpy.mock.calls[0]?.[2];
    if (buttons === undefined) throw new Error('no delete prompt was shown');
    return buttons;
  }

  function pressButton(style: AlertButton['style']): void {
    act(() =>
      promptButtons()
        .find((button) => button.style === style)
        ?.onPress?.(),
    );
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    __http.reply(DELETE_ROUTE, { status: HTTP_NO_CONTENT });
    alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  });

  afterEach(() => {
    alertSpy.mockRestore();
    queryClient.clear();
  });

  describe('the delete prompt', () => {
    it('asks before deleting and sends nothing until the user confirms', () => {
      openDeletePrompt(createMemoryNavigator('/library/playlist/pl1'));

      expect(alertSpy.mock.calls[0]?.slice(0, TITLE_AND_MESSAGE)).toEqual([
        'Delete Playlist',
        'This cannot be undone.',
      ]);
      expect(promptButtons().map((button) => button.text)).toEqual(['Cancel', 'Delete']);
      expect(__http.countFor(DELETE_ROUTE)).toBe(0);
    });

    it('sends nothing and stays put when the user cancels', () => {
      const navigator = createMemoryNavigator('/library/playlist/pl1');
      openDeletePrompt(navigator);

      pressButton('cancel');

      expect(__http.countFor(DELETE_ROUTE)).toBe(0);
      expect(navigator.current()).toBe('/library/playlist/pl1');
    });
  });

  describe('confirming the delete', () => {
    async function deleteThrough(navigator: MemoryNavigator): Promise<void> {
      openDeletePrompt(navigator);
      pressButton('destructive');
      await waitFor(() => expect(navigator.current()).toBe('/library'));
    }

    it('sends one delete for the playlist', async () => {
      await deleteThrough(createMemoryNavigator('/library/playlist/pl1'));

      expect(__http.countFor(DELETE_ROUTE)).toBe(1);
    });

    it('marks the cached playlist list stale so it refetches without the deleted one', async () => {
      queryClient.setQueryData(playlistKeys.list, { items: [] });

      await deleteThrough(createMemoryNavigator('/library/playlist/pl1'));

      expect(queryClient.getQueryState(playlistKeys.list)?.isInvalidated).toBe(true);
    });

    it('pops back to the previous screen when history exists', async () => {
      const navigator = createMemoryNavigator('/library');
      navigator.push('/library/playlist/pl1');

      await deleteThrough(navigator);

      expect(navigator.canGoBack()).toBe(false);
    });

    it('lands on the library when there is no history', async () => {
      const navigator = createMemoryNavigator('/library/playlist/pl1');

      await deleteThrough(navigator);

      expect(navigator.current()).toBe('/library');
    });
  });
});

describe('usePlaylistOfflineAction', () => {
  function track(id: string, status: TrackResponse['acquisition_status'] = 'ready'): TrackResponse {
    return trackResponseFactory.build({
      id: asTrackId(id),
      title: `Track ${id}`,
      artist: 'An Artist',
      album: null,
      duration_seconds: 180,
      added_at: '2024-01-01T00:00:00Z',
      acquisition_status: status,
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    });
  }

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('usePlaylistOfflineAction', () => {
    const pinMany = jest.fn().mockResolvedValue({ requested: 0, failed: 0 });
    const unpinMany = jest.fn().mockResolvedValue({ requested: 0, failed: 0 });

    function menuFor(tracks: TrackResponse[], readyIds: string[]) {
      const entries = Object.fromEntries(
        readyIds.map((id) => [
          id,
          {
            trackId: asTrackId(id),
            status: 'ready' as const,
            uri: `file:///offline-audio/${id}.mp3`,
          },
        ]),
      );
      usePinnedStore.setState({ entries, pinMany, unpinMany });
      return renderHook(() => usePlaylistOfflineAction(tracks)).result.current;
    }

    it('has nothing to download when no track is ready', () => {
      const item = menuFor([track('a', 'failed')], []);
      expect(item.label).toBe('Nothing to download yet');
      item.onPress();
      expect(pinMany).not.toHaveBeenCalled();
    });

    it('downloads all ready tracks when none are pinned', () => {
      const item = menuFor([track('a'), track('b'), track('c', 'failed')], []);
      expect(item.label).toBe('Download all (2)');
      item.onPress();
      expect(pinMany).toHaveBeenCalledWith(['a', 'b']);
    });

    it('downloads the rest when some are pinned', () => {
      const item = menuFor([track('a'), track('b'), track('c')], ['a']);
      expect(item.label).toBe('Download rest (2)');
    });

    it('removes every download in one bulk call when every ready track is pinned', () => {
      const item = menuFor([track('a'), track('b'), track('c', 'failed')], ['a', 'b']);
      expect(item.label).toBe('Remove downloads');
      item.onPress();
      expect(unpinMany.mock.calls).toEqual([[['a', 'b']]]);
    });

    it('summarizes a partial removal as "N of M downloads could not be removed"', async () => {
      const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
      unpinMany.mockResolvedValueOnce({ requested: 2, failed: 1 });
      const item = menuFor([track('a'), track('b')], ['a', 'b']);
      item.onPress();
      await act(async () => {
        await new Promise<void>((resolve) => setImmediate(resolve));
      });
      expect(alert).toHaveBeenCalledTimes(1);
      expect(alert.mock.calls[0]?.[1]).toContain('1 of 2 downloads could not be removed');
      alert.mockRestore();
    });

    it('summarizes a mixed batch as "N of M downloads failed" once it settles', async () => {
      const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
      pinMany.mockResolvedValueOnce({ requested: 2, failed: 1 });
      const item = menuFor([track('a'), track('b')], []);
      item.onPress();
      await act(async () => {
        await new Promise<void>((resolve) => setImmediate(resolve));
      });
      expect(alert).toHaveBeenCalledTimes(1);
      expect(alert.mock.calls[0]?.[1]).toContain('1 of 2 downloads failed');
      alert.mockRestore();
    });
  });
});
