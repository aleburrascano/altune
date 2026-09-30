import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/lib/query-keys';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { useSaveTrack } from '../hooks/useSaveTrack';
import { createTestQueryClient, createWrapper } from './support/queryHarness';

const mockCreateTrack = jest.fn<Promise<TrackResponse>, [unknown]>();

jest.mock('@shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
}));

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

describe('seeding download meta', () => {
  function saved(): TrackResponse {
    return {
      id: asTrackId('server-1'),
      title: 'Idioteque',
      artist: 'Radiohead',
      album: null,
      duration_seconds: 300,
      added_at: '2024-01-01T00:00:00Z',
      acquisition_status: 'pending',
      artwork_url: 'https://cdn/idioteque.png',
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    } as TrackResponse;
  }

  function setup() {
    return { wrapper: createWrapper(createTestQueryClient({ mutations: true })) };
  }

  beforeEach(() => {
    mockCreateTrack.mockReset();
    useTrackStatusStore.getState().reset();
    useDownloadStore.getState().reset();
  });

  afterEach(() => {
    useDownloadStore.getState().reset();
  });

  describe('useSaveTrack — download meta', () => {
    it('remembers the real title/artist/artwork under the real id once the save lands', async () => {
      const { wrapper } = setup();
      mockCreateTrack.mockResolvedValue(saved());

      const { result } = renderHook(() => useSaveTrack(), { wrapper });
      await act(async () => {
        await result.current.mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never);
      });

      useDownloadStore.getState().start(asTrackId('server-1'));

      expect(useDownloadStore.getState().entries['server-1']).toEqual({
        trackId: 'server-1',
        phase: 'finding',
        title: 'Idioteque',
        artist: 'Radiohead',
        artworkUrl: 'https://cdn/idioteque.png',
      });
    });

    it('remembers nothing when the save fails', async () => {
      const { wrapper } = setup();
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      mockCreateTrack.mockRejectedValue(new Error('502 bad gateway'));

      const { result } = renderHook(() => useSaveTrack(), { wrapper });
      await act(async () => {
        await result.current
          .mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never)
          .catch(() => undefined);
      });

      expect(useDownloadStore.getState().remembered).toEqual({});
      warnSpy.mockRestore();
    });
  });
});

describe('invalidating derived caches', () => {
  function saved(): TrackResponse {
    return {
      id: asTrackId('server-1'),
      title: 'Idioteque',
      artist: 'Radiohead',
      album: null,
      duration_seconds: 300,
      added_at: '2024-01-01T00:00:00Z',
      acquisition_status: 'pending',
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    } as TrackResponse;
  }

  function setup() {
    const queryClient = createTestQueryClient({ mutations: true });
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    return { spy, wrapper: createWrapper(queryClient) };
  }

  function invalidatedKeys(spy: jest.SpyInstance): unknown[] {
    return spy.mock.calls.map(([filters]) => (filters as { queryKey: unknown }).queryKey);
  }

  beforeEach(() => {
    mockCreateTrack.mockReset();
    useTrackStatusStore.getState().reset();
  });

  describe('useSaveTrack — derived caches', () => {
    it('invalidates every membership-derived cache once the save lands', async () => {
      const { spy, wrapper } = setup();
      mockCreateTrack.mockResolvedValue(saved());

      const { result } = renderHook(() => useSaveTrack(), { wrapper });
      await act(async () => {
        await result.current.mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never);
      });

      expect(invalidatedKeys(spy)).toEqual([
        libraryKeys.albumsPrefix,
        libraryKeys.artistsPrefix,
        libraryKeys.summary,
        libraryKeys.lookupPrefix,
      ]);
    });

    it('invalidates nothing when the save fails', async () => {
      const { spy, wrapper } = setup();
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      mockCreateTrack.mockRejectedValue(new Error('502 bad gateway'));

      const { result } = renderHook(() => useSaveTrack(), { wrapper });
      await act(async () => {
        await result.current
          .mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never)
          .catch(() => undefined);
      });

      expect(spy).not.toHaveBeenCalled();
      warnSpy.mockRestore();
    });
  });
});

describe('logging a failed save', () => {
  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    jest.clearAllMocks();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  describe('useSaveTrack logs the reason a save failed', () => {
    it('logs the error message and track identity on save failure', async () => {
      mockCreateTrack.mockRejectedValue(new Error('502 bad gateway'));

      const { result } = renderHook(() => useSaveTrack(), {
        wrapper: createWrapper(createTestQueryClient()),
      });

      await act(async () => {
        await result.current
          .mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never)
          .catch(() => {});
      });

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] save track failed',
        expect.objectContaining({
          title: 'Idioteque',
          artist: 'Radiohead',
          error: '502 bad gateway',
        }),
      );
    });
  });
});

describe('a save in flight at sign-out', () => {
  it('leaves the status store and outbox untouched when it resolves afterwards', async () => {
    const queryClient = createTestQueryClient({ mutations: true });
    let resolveSave: (track: TrackResponse) => void = () => undefined;
    mockCreateTrack.mockReset();
    mockCreateTrack.mockReturnValue(
      new Promise<TrackResponse>((resolve) => {
        resolveSave = resolve;
      }),
    );
    useTrackStatusStore.getState().reset();
    (enqueueCritical as jest.Mock).mockClear();

    const { result } = renderHook(() => useSaveTrack(), {
      wrapper: createWrapper(queryClient),
    });
    let pendingSave: Promise<unknown> = Promise.resolve();
    act(() => {
      pendingSave = result.current
        .mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never)
        .catch(() => undefined);
    });
    act(() => {
      runSignOutCleanups();
    });
    const statusesAfterSignOut = useTrackStatusStore.getState().statuses;
    await act(async () => {
      resolveSave({
        id: asTrackId('server-1'),
        title: 'Idioteque',
        artist: 'Radiohead',
        acquisition_status: 'pending',
      } as TrackResponse);
      await pendingSave;
    });

    expect(useTrackStatusStore.getState().statuses).toBe(statusesAfterSignOut);
    expect(enqueueCritical).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: 'library_add' }),
    );
  });
});
