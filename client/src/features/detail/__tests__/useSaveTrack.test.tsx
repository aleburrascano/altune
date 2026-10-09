import { act, renderHook, waitFor } from '@testing-library/react-native';
import { ApiError } from '@shared/failure/errors';
import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/query/queryKeys';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { enqueueCritical } from '@shared/telemetry/outbox';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useSaveTrack, useTrackSave } from '@/features/detail/hooks/useSaveTrack';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { createMemoryDetailPort } from '@/features/detail/__tests__/memoryDetailPort';
import {
  createPortWrapper,
  createTestQueryClient,
} from '@/features/detail/__tests__/support/queryHarness';
import { toCreateTrackRequest } from '@/features/detail/domain/saveTrack';
import { ownedTrack } from '@/features/detail/domain/ownedTrack';

const mockCreateTrack = jest.fn<Promise<TrackResponse>, [unknown]>();

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const mockRetryAcquisition = jest.fn<Promise<void>, [unknown]>();

jest.mock('@/features/detail/domain/saveTrack', () => ({
  ...jest.requireActual('@/features/detail/domain/saveTrack'),
  toCreateTrackRequest: jest.fn(
    jest.requireActual('@/features/detail/domain/saveTrack').toCreateTrackRequest,
  ),
}));

describe('useSaveTrack', () => {
  function createWrapper(queryClient: ReturnType<typeof createTestQueryClient>) {
    const detail = {
      ...createMemoryDetailPort(),
      createTrack: (body: unknown) => mockCreateTrack(body),
    };
    return createPortWrapper(queryClient, { detail: detail as DetailPort });
  }

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

  describe('two same-title saves on different albums in flight together', () => {
    type Settle = { resolve: (track: TrackResponse) => void; reject: (error: Error) => void };

    async function startBoth() {
      const settles: Record<string, Settle> = {};
      mockCreateTrack.mockReset();
      mockCreateTrack.mockImplementation((body) => {
        const album = (body as { album: string }).album;
        return new Promise<TrackResponse>((resolve, reject) => {
          settles[album] = { resolve, reject };
        });
      });
      useTrackStatusStore.getState().reset();
      const queryClient = createTestQueryClient({ mutations: true });
      const { result } = renderHook(() => useSaveTrack(), { wrapper: createWrapper(queryClient) });
      await act(async () => {
        result.current.mutate({ title: 'Song', artist: 'Artist', album: 'Album' } as never);
        result.current.mutate({ title: 'Song', artist: 'Artist', album: 'Hits' } as never);
      });
      return settles;
    }

    function savedOn(album: string): TrackResponse {
      return {
        id: asTrackId(`server-${album}`),
        title: 'Song',
        artist: 'Artist',
        album,
        acquisition_status: 'pending',
      } as TrackResponse;
    }

    function failedIds(): string[] {
      const { statuses } = useTrackStatusStore.getState();
      return Object.keys(statuses).filter(
        (id) => statuses[id as keyof typeof statuses]?.acquisitionStatus === 'failed',
      );
    }

    it.each([
      ['rejected first', ['Album', 'Hits']],
      ['rejected last', ['Hits', 'Album']],
    ])('marks only the rejected save failed when it is %s', async (_label, order) => {
      const settles = await startBoth();
      const [first, second] = order as [string, string];

      await act(async () => {
        settles[first]!.reject(new Error('boom'));
      });
      await act(async () => {
        settles[second]!.resolve(savedOn(second));
      });

      expect(failedIds()).toHaveLength(1);
      expect(useTrackStatusStore.getState().statuses[asTrackId(`server-${second}`)]).toBeDefined();
    });

    it('keeps the survivor placeholder status while the other save fails', async () => {
      const settles = await startBoth();
      const pendingBefore = Object.keys(useTrackStatusStore.getState().statuses);

      await act(async () => {
        settles['Album']!.reject(new Error('boom'));
      });

      expect(pendingBefore).toHaveLength(2);
      expect(failedIds()).toHaveLength(1);
      const statuses = useTrackStatusStore.getState().statuses;
      const survivors = Object.keys(statuses).filter(
        (id) => statuses[id as keyof typeof statuses]?.acquisitionStatus === 'pending',
      );
      expect(survivors).toHaveLength(1);
    });
  });

  describe('saving through the memory fake', () => {
    const request = { title: 'Idioteque', artist: 'Radiohead' } as never;

    function setup(port: DetailPort) {
      const wrapper = createPortWrapper(createTestQueryClient({ mutations: true }), {
        detail: port,
      });
      return renderHook(() => useSaveTrack(), { wrapper });
    }

    function trackCount(port: DetailPort): Promise<number> {
      return port.getTracks({ limit: 50, offset: 0 }).then((page) => page.items.length);
    }

    beforeEach(() => {
      useTrackStatusStore.getState().reset();
    });

    it('puts the new track into the fake, which held none before', async () => {
      const port = createMemoryDetailPort();
      expect(await trackCount(port)).toBe(0);
      const { result } = setup(port);

      await act(async () => {
        await result.current.mutateAsync(request);
      });

      expect(await trackCount(port)).toBe(1);
    });

    it('reuses the idempotency key on a second save of the same result', async () => {
      const port = createMemoryDetailPort();
      const { result } = setup(port);

      await act(async () => {
        await result.current.mutateAsync(request);
        await result.current.mutateAsync(request);
      });

      expect(await trackCount(port)).toBe(1);
    });

    it.each([
      [503, true],
      [404, false],
    ])('reports a %i rejection as retryable=%s', async (status, retryable) => {
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      const port: DetailPort = {
        ...createMemoryDetailPort(),
        createTrack: () => Promise.reject(new ApiError(status, `${status} failed`)),
      };
      const { result } = setup(port);

      await act(async () => {
        await result.current.mutateAsync(request).catch(() => undefined);
      });

      await waitFor(() => expect(result.current.failure).not.toBeNull());
      expect(result.current.failure?.isRetryable).toBe(retryable);
      warnSpy.mockRestore();
    });
  });

  describe('useSaveTrack — terminal status delivered before the save response', () => {
    const serverId = asTrackId('server-race');

    function pendingResponse(): TrackResponse {
      return {
        id: serverId,
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

    beforeEach(() => {
      mockCreateTrack.mockReset();
      useTrackStatusStore.getState().reset();
    });

    it.each([
      ['ready', null],
      ['failed', 'no match'],
    ] as const)('keeps %s when the event beat a pending response', async (status, message) => {
      const queryClient = createTestQueryClient({ mutations: true });
      mockCreateTrack.mockImplementation(() => {
        useTrackStatusStore
          .getState()
          .patch(serverId, { acquisitionStatus: status, failureMessage: message } as never);
        return Promise.resolve(pendingResponse());
      });

      const { result } = renderHook(() => useSaveTrack(), { wrapper: createWrapper(queryClient) });
      await act(async () => {
        await result.current.mutateAsync({ title: 'Idioteque', artist: 'Radiohead' } as never);
      });

      expect(useTrackStatusStore.getState().statuses[serverId]).toEqual({
        acquisitionStatus: status,
        failureMessage: message,
      });
    });
  });
});

describe('useTrackSave', () => {
  let wrapper: ReturnType<typeof createPortWrapper>;

  beforeEach(() => {
    wrapper = createPortWrapper(createTestQueryClient({ mutations: true }), {
      detail: { ...createMemoryDetailPort(), createTrack: (body) => mockCreateTrack(body) },
      acquisition: { retryAcquisition: (trackId) => mockRetryAcquisition(trackId) },
    });
  });

  function track(subtitle: string | null = 'Radiohead'): DiscoveryResult {
    return {
      kind: 'track',
      title: 'Idioteque',
      subtitle,
      image_url: null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
  }

  beforeEach(() => {
    mockCreateTrack.mockReset();
    mockRetryAcquisition.mockReset();
    useTrackStatusStore.getState().reset();
  });

  describe('useTrackSave state derivation', () => {
    it('is disabled with no failure when the track has no known artist', () => {
      const { result } = renderHook(() => useTrackSave(track(null), null), { wrapper });

      expect(result.current.state).toBe('disabled');
      expect(result.current.failure).toBeNull();
    });

    it('is add for an unowned, unattempted track', () => {
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      expect(result.current.state).toBe('add');
    });

    it('is ready for a track already in the library', () => {
      const owned = ownedTrack(asTrackId('server-1'), 'ready', null);
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      expect(result.current.state).toBe('ready');
    });

    it('is saving while an owned acquisition is pending', () => {
      const owned = ownedTrack(asTrackId('server-1'), 'pending', null);
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      expect(result.current.state).toBe('saving');
    });

    it('is failed, with the remembered reason, for a track the server previously failed', () => {
      const owned = ownedTrack(asTrackId('server-1'), 'failed', 'network error');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      expect(result.current.state).toBe('failed');
      expect(result.current.failure).toEqual({ message: 'network error', isRetryable: true });
    });

    it('is saving once onSave dispatches a create for an unowned track', async () => {
      mockCreateTrack.mockReturnValue(new Promise(() => undefined));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(result.current.state).toBe('saving'));
    });

    it('is failed for a retryable create error, saving from add', async () => {
      mockCreateTrack.mockRejectedValue(new ApiError(503, '503 unavailable'));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(result.current.state).toBe('failed'));
      expect(result.current.failure).toEqual({
        message: '503 unavailable',
        isRetryable: true,
        trackId: expect.anything(),
      });
    });

    it('is rejected, with no retry offered, for a permanent create refusal', async () => {
      mockCreateTrack.mockRejectedValue(new ApiError(400, '400 duplicate_track'));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(result.current.state).toBe('rejected'));
    });
  });

  describe('useTrackSave onSave', () => {
    it('does not call the retry or create endpoints when the state is disabled', () => {
      const { result } = renderHook(() => useTrackSave(track(null), null), { wrapper });

      act(() => {
        result.current.onSave();
      });

      expect(mockCreateTrack).not.toHaveBeenCalled();
      expect(mockRetryAcquisition).not.toHaveBeenCalled();
    });

    it('does not re-dispatch a create for a permanently rejected track', async () => {
      mockCreateTrack.mockRejectedValue(new ApiError(400, '400 duplicate_track'));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
      });
      await waitFor(() => expect(result.current.state).toBe('rejected'));
      mockCreateTrack.mockClear();

      act(() => {
        result.current.onSave();
      });

      expect(mockCreateTrack).not.toHaveBeenCalled();
    });

    it('hits the retry endpoint with the owned trackId, never a create, for a failed owned track', async () => {
      mockRetryAcquisition.mockResolvedValue(undefined);
      const trackId = asTrackId('server-1');
      const owned = ownedTrack(trackId, 'failed', 'network error');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledWith(trackId));
      expect(mockCreateTrack).not.toHaveBeenCalled();
    });
  });

  function savedTrack(): TrackResponse {
    return {
      id: 'srv-idioteque',
      title: 'Idioteque',
      artist: 'Radiohead',
      album: null,
      duration_seconds: null,
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

  describe('useTrackSave repeated saves', () => {
    it('dispatches one create when onSave is called again while the first is saving', async () => {
      mockCreateTrack.mockReturnValue(new Promise(() => undefined));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
      });
      await waitFor(() => expect(result.current.state).toBe('saving'));
      act(() => {
        result.current.onSave();
      });

      expect(mockCreateTrack).toHaveBeenCalledTimes(1);
    });

    it('dispatches a second create on retry after a retryable failure, and clears the failure', async () => {
      mockCreateTrack.mockRejectedValueOnce(new ApiError(503, '503 unavailable'));
      mockCreateTrack.mockResolvedValueOnce(savedTrack());
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
      });
      await waitFor(() => expect(result.current.state).toBe('failed'));
      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(mockCreateTrack).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(result.current.failure).toBeNull());
      expect(result.current.state).not.toBe('failed');
    });

    it('never dispatches when a track with no known artist is saved repeatedly', () => {
      const { result } = renderHook(() => useTrackSave(track(''), null), { wrapper });

      act(() => {
        result.current.onSave();
        result.current.onSave();
      });

      expect(result.current.state).toBe('disabled');
      expect(mockCreateTrack).not.toHaveBeenCalled();
    });
  });

  describe('useTrackSave double tap', () => {
    it('issues exactly one create for two taps in one act', async () => {
      mockCreateTrack.mockReturnValue(new Promise(() => undefined));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
        result.current.onSave();
      });

      await waitFor(() => expect(result.current.state).toBe('saving'));
      expect(mockCreateTrack).toHaveBeenCalledTimes(1);
    });

    it('calls retryAcquisition once for two taps on a failed owned track', async () => {
      mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
      const owned = ownedTrack(asTrackId('server-1'), 'failed', 'network error');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      act(() => {
        result.current.onSave();
        result.current.onSave();
      });

      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledTimes(1));
    });

    it('re-arms after a double-tapped create fails', async () => {
      mockCreateTrack.mockRejectedValue(new ApiError(503, '503 unavailable'));
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      act(() => {
        result.current.onSave();
        result.current.onSave();
      });
      await waitFor(() => expect(result.current.state).toBe('failed'));
      await act(async () => {
        await Promise.resolve();
      });
      act(() => {
        result.current.onSave();
        result.current.onSave();
      });

      await waitFor(() => expect(mockCreateTrack).toHaveBeenCalledTimes(2));
      await act(async () => {
        await Promise.resolve();
      });
      expect(mockCreateTrack).toHaveBeenCalledTimes(2);
    });

    it('retries again after a double-tapped retry fails', async () => {
      mockRetryAcquisition.mockRejectedValue(new ApiError(503, '503 unavailable'));
      const trackId = asTrackId('server-1');
      const owned = ownedTrack(trackId, 'failed', 'network error');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      act(() => {
        result.current.onSave();
        result.current.onSave();
      });
      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledTimes(1));
      await waitFor(() => expect(result.current.state).toBe('failed'));
      await act(async () => {
        await Promise.resolve();
      });

      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledTimes(2));
    });

    it('dispatches a create again after building the create request throws', async () => {
      mockCreateTrack.mockReturnValue(new Promise(() => undefined));
      jest.mocked(toCreateTrackRequest).mockImplementationOnce(() => {
        throw new Error('bad request build');
      });
      const { result } = renderHook(() => useTrackSave(track(), null), { wrapper });

      expect(() => result.current.onSave()).toThrow('bad request build');
      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(mockCreateTrack).toHaveBeenCalledTimes(1));
    });

    it('retries again after the retry telemetry throws', async () => {
      mockRetryAcquisition.mockReturnValue(new Promise(() => undefined));
      jest.mocked(enqueueCritical).mockImplementationOnce(() => {
        throw new Error('outbox down');
      });
      const owned = ownedTrack(asTrackId('server-1'), 'failed', 'network error');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      expect(() => result.current.onSave()).toThrow('outbox down');
      act(() => {
        result.current.onSave();
      });

      await waitFor(() => expect(mockRetryAcquisition).toHaveBeenCalledTimes(1));
    });
  });

  describe('useTrackSave remembered failures', () => {
    it('is rejected after a remount when the stored failure was not retryable', () => {
      const owned = ownedTrack(asTrackId('server-1'), 'failed', 'bad request', false);
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      expect(result.current.state).toBe('rejected');
      expect(result.current.failure?.isRetryable).toBe(false);
    });

    it('is failed after a remount when the stored failure was retryable', () => {
      const owned = ownedTrack(asTrackId('server-1'), 'failed', 'unavailable');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      expect(result.current.state).toBe('failed');
      expect(result.current.failure?.isRetryable).toBe(true);
    });

    it('keeps the retryable flag in the store when a retry is rejected', async () => {
      mockRetryAcquisition.mockRejectedValue(new ApiError(422, '422 unprocessable'));
      const trackId = asTrackId('server-1');
      const owned = ownedTrack(trackId, 'failed', 'network error');
      const { result } = renderHook(() => useTrackSave(track(), owned), { wrapper });

      act(() => {
        result.current.onSave();
      });

      await waitFor(() =>
        expect(useTrackStatusStore.getState().statuses[trackId]).toMatchObject({
          acquisitionStatus: 'failed',
          retryable: false,
        }),
      );
    });
  });
});
