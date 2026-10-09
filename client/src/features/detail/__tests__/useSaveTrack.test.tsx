import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/errors';
import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/query/queryKeys';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { useSaveTrack } from '../hooks/useSaveTrack';
import type { DetailPort } from '../ports/detailPort';
import { createMemoryDetailPort } from './memoryDetailPort';
import { createPortWrapper, createTestQueryClient } from './support/queryHarness';

const mockCreateTrack = jest.fn<Promise<TrackResponse>, [unknown]>();

function createWrapper(queryClient: ReturnType<typeof createTestQueryClient>) {
  const detail = {
    ...createMemoryDetailPort(),
    createTrack: (body: unknown) => mockCreateTrack(body),
  };
  return createPortWrapper(queryClient, { detail: detail as DetailPort });
}

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
    const wrapper = createPortWrapper(createTestQueryClient({ mutations: true }), { detail: port });
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
