import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';
import { asPlaylistId, asTrackId } from '@shared/catalog';
import type { Navigator } from '@shared/navigation';
import { createMemoryNavigator } from '@testing/fakes/navigation/inMemoryNavigator';
import {
  LibraryPortProvider,
  usePlaylistDelete,
  usePlaylistOfflineAction,
} from '@features/library';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';
import {
  recordingDialogs,
  type RecordingDialogs,
} from '@features/library/__tests__/recordingDialogs';
import type { TrackResponse } from '@shared/catalog';
import { usePinnedStore } from '@shared/offline';
import { trackResponseFactory } from '@testing/factories';
import { signInTestUser } from '@testing/signInTestUser';
import { PlaylistsPortProvider } from '@shared/playlists';
import { apiClientPlaylistsPort } from '@testing/apiClientPorts';
import { flushPromises } from '@testing/flushPromises';
import { EVENT_ROUTES } from '@testing/eventRoutes';

const { __http } = require('@testing/doubles/fetch.js');

const DELETE_ROUTE = 'DELETE /v1/playlists/pl1';
const TELEMETRY_ROUTE = EVENT_ROUTES;
const HTTP_NO_CONTENT = 204;
const HTTP_ACCEPTED = 202;

type MemoryNavigator = ReturnType<typeof createMemoryNavigator>;

interface WrapperProps {
  children: ReactNode;
}

function libraryPortAround(dialogs: RecordingDialogs, children: ReactNode) {
  const port = createMemoryLibraryPort({
    showFailure: dialogs.showFailure,
    confirmDestructive: dialogs.confirmDestructive,
  });
  return <LibraryPortProvider port={port}>{children}</LibraryPortProvider>;
}

beforeEach(async () => {
  await signInTestUser();
  __http.reply(TELEMETRY_ROUTE, { status: HTTP_ACCEPTED });
});

describe('usePlaylistDelete', () => {
  const PLAYLIST_ID = asPlaylistId('pl1');
  let queryClient: QueryClient;
  let dialogs: RecordingDialogs;

  function wrapper({ children }: WrapperProps) {
    return (
      <QueryClientProvider client={queryClient}>
        <PlaylistsPortProvider port={apiClientPlaylistsPort}>
          {libraryPortAround(dialogs, children)}
        </PlaylistsPortProvider>
      </QueryClientProvider>
    );
  }

  function openDeletePrompt(navigator: Navigator): void {
    const { result } = renderHook(() => usePlaylistDelete(PLAYLIST_ID, navigator), { wrapper });
    act(() => result.current());
  }

  function confirmPrompt(): void {
    const prompt = dialogs.prompts[0];
    if (prompt === undefined) throw new Error('no delete prompt was shown');
    act(() => prompt.onConfirm());
  }

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    dialogs = recordingDialogs();
    __http.reply(DELETE_ROUTE, { status: HTTP_NO_CONTENT });
  });

  afterEach(() => {
    queryClient.clear();
  });

  describe('the delete prompt', () => {
    it('asks before deleting, then sends nothing and stays put until the user confirms', () => {
      const navigator = createMemoryNavigator('/library/playlist/pl1');
      openDeletePrompt(navigator);

      expect(dialogs.prompts).toEqual([
        {
          title: 'Delete Playlist',
          message: 'This cannot be undone.',
          confirmLabel: 'Delete',
          onConfirm: expect.any(Function),
        },
      ]);
      expect(__http.countFor(DELETE_ROUTE)).toBe(0);
      expect(navigator.current()).toBe('/library/playlist/pl1');
    });
  });

  describe('confirming the delete', () => {
    async function deleteThrough(navigator: MemoryNavigator): Promise<void> {
      openDeletePrompt(navigator);
      confirmPrompt();
      await waitFor(() => expect(navigator.current()).toBe('/library'));
    }

    it('sends one delete for the playlist', async () => {
      await deleteThrough(createMemoryNavigator('/library/playlist/pl1'));

      expect(__http.countFor(DELETE_ROUTE)).toBe(1);
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

  let dialogs: RecordingDialogs;

  function wrapper({ children }: WrapperProps) {
    return libraryPortAround(dialogs, children);
  }

  beforeEach(() => {
    jest.clearAllMocks();
    dialogs = recordingDialogs();
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
      return renderHook(() => usePlaylistOfflineAction(tracks), { wrapper }).result.current;
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
      unpinMany.mockResolvedValueOnce({ requested: 2, failed: 1 });
      const item = menuFor([track('a'), track('b')], ['a', 'b']);
      item.onPress();
      await act(async () => {
        await flushPromises();
      });
      expect(dialogs.alerts).toHaveLength(1);
      expect(dialogs.alerts[0]?.message).toContain('1 of 2 downloads could not be removed');
    });

    it('summarizes a mixed batch as "N of M downloads failed" once it settles', async () => {
      pinMany.mockResolvedValueOnce({ requested: 2, failed: 1 });
      const item = menuFor([track('a'), track('b')], []);
      item.onPress();
      await act(async () => {
        await flushPromises();
      });
      expect(dialogs.alerts).toHaveLength(1);
      expect(dialogs.alerts[0]?.message).toContain('1 of 2 downloads failed');
    });
  });
});
