import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/lib/query-keys';

import { useSaveTrack } from '../hooks/useSaveTrack';

const mockCreateTrack = jest.fn<Promise<TrackResponse>, [unknown]>();

jest.mock('@shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
}));

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

describe('seeding download meta', () => {
  // A save's response is the real TrackResponse under the real id — the same shape
  // the SSE track_added_to_library carries. It should seed the download store the
  // same way, so a started/failed acquisition event that arrives with nothing in the
  // query caches still shows the real title (#downloads-bar-title).

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
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    function Wrapper({ children }: { children: React.ReactNode }) {
      return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    }
    return { wrapper: Wrapper };
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
  // A save changes library membership, so it must mark the same derived caches stale
  // as every other add/delete site (#938) — including the library summary, which the
  // hand-rolled key list here used to skip.

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
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    function Wrapper({ children }: { children: React.ReactNode }) {
      return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    }
    const spy = jest.spyOn(queryClient, 'invalidateQueries');
    return { spy, wrapper: Wrapper };
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
  // These detail hooks each have a silent failure path: the failure reason is
  // discarded with no log, so a real incident can't be told apart from a one-off
  // without a live repro. Each test drives its hook into that failure path and
  // asserts the site now logs enough to diagnose it (status/provider/artist, the
  // save error + track identity, the lateral-nav query/kind + error, the entity a
  // failed discovery search was looking for).

  function createWrapper(queryClient: QueryClient) {
    return function Wrapper({ children }: { children: React.ReactNode }) {
      return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    };
  }

  function freshClient() {
    return new QueryClient({ defaultOptions: { queries: { retry: false } } });
  }

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
        wrapper: createWrapper(freshClient()),
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
