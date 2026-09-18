import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { useArtistContent } from '../hooks/useArtistContent';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { useLateralNav } from '../hooks/useLateralNav';

// These three detail hooks each have a silent failure path: the failure reason
// is discarded with no log, so a real incident can't be told apart from a
// one-off without a live repro. Each test drives its hook into that failure
// path and asserts the site now logs enough to diagnose it (status/provider/
// artist, the save error + track identity, the lateral-nav query/kind + error).

const mockGetArtistContent = jest.fn();
jest.mock('@shared/api-client/enrichment', () => ({
  getArtistContent: (...args: unknown[]) => mockGetArtistContent(...args),
}));

const mockCreateTrack = jest.fn();
jest.mock('@shared/api-client/tracks', () => ({
  createTrack: (...args: unknown[]) => mockCreateTrack(...args),
}));

// Isolate useSaveTrack's onError logging from its cache/store side effects.
jest.mock('@shared/acquisition/trackStatusStore', () => ({
  linkTrackIdentity: jest.fn(),
  patchTrackStatus: jest.fn(),
  removeTrackStatus: jest.fn(),
  trackIdentityKey: () => 'identity',
}));
jest.mock('@shared/events/trackCachePatch', () => ({
  invalidateLibraryDerived: jest.fn(),
  removeTrackFromCaches: jest.fn(),
  replaceTrackInCaches: jest.fn(),
  upsertTrackInCaches: jest.fn(),
}));
jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));
jest.mock('../save-cache', () => ({
  optimisticTrack: (body: { title: string; artist: string }) => ({
    id: 'optimistic-1',
    title: body.title,
    artist: body.artist,
  }),
  saveIdempotencyKey: () => 'save-key',
}));

const mockResolveEntityQuery = jest.fn();
jest.mock('../resolve-entity-query', () => ({
  resolveEntityQuery: (...args: unknown[]) => mockResolveEntityQuery(...args),
}));
jest.mock('../navigation', () => ({
  tabRootFromSegments: () => 'discover',
  detailRouteFor: () => '/discover/detail',
  openDetail: jest.fn(),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
  useSegments: () => ['(tabs)', 'discover'],
}));

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

function contentResponse(overrides: {
  topStatus?: string;
  albumsStatus?: string;
}): unknown {
  return {
    top_tracks: { items: [], provider: 'spotify', status: overrides.topStatus ?? 'ok', latency_ms: 1 },
    albums: { items: [], provider: 'spotify', status: overrides.albumsStatus ?? 'ok', latency_ms: 1 },
  };
}

describe('useArtistContent logs degraded per-provider content statuses', () => {
  it('logs which side/provider/artist failed when top_tracks status is not ok', async () => {
    mockGetArtistContent.mockResolvedValue(contentResponse({ topStatus: 'timeout' }));

    const { result } = renderHook(
      () =>
        useArtistContent({
          sources: [{ provider: 'spotify', external_id: 'artist-1', url: 'https://x' }],
          artistName: 'Radiohead',
        }),
      { wrapper: createWrapper(freshClient()) },
    );

    await waitFor(() => expect(result.current.isErrorTracks).toBe(true), { timeout: 5000 });

    expect(warnSpy).toHaveBeenCalledWith(
      '[detail] artist top_tracks fetch degraded',
      expect.objectContaining({
        status: 'timeout',
        provider: 'spotify',
        externalId: 'artist-1',
        artistName: 'Radiohead',
      }),
    );
  });

  it('logs when the whole content request throws', async () => {
    mockGetArtistContent.mockRejectedValue(new Error('network down'));

    const { result } = renderHook(
      () =>
        useArtistContent({
          sources: [{ provider: 'deezer', external_id: 'artist-2', url: 'https://x' }],
        }),
      { wrapper: createWrapper(freshClient()) },
    );

    await waitFor(() => expect(result.current.isErrorTracks).toBe(true), { timeout: 5000 });

    expect(warnSpy).toHaveBeenCalledWith(
      '[detail] artist content fetch failed',
      expect.objectContaining({
        provider: 'deezer',
        externalId: 'artist-2',
        error: 'network down',
      }),
    );
  });

  it('does not log when both statuses are ok', async () => {
    mockGetArtistContent.mockResolvedValue(contentResponse({}));

    const { result } = renderHook(
      () =>
        useArtistContent({
          sources: [{ provider: 'spotify', external_id: 'artist-3', url: 'https://x' }],
        }),
      { wrapper: createWrapper(freshClient()) },
    );

    await waitFor(() => expect(result.current.isLoadingTracks).toBe(false), { timeout: 5000 });

    expect(warnSpy).not.toHaveBeenCalled();
  });
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

describe('useLateralNav logs non-"not found" fetch failures', () => {
  it('logs the query/kind and error when the resolve fetch throws', async () => {
    mockResolveEntityQuery.mockReturnValue({
      queryKey: ['resolve-entity', 'artist', 'Boom', 1],
      queryFn: () => Promise.reject(new Error('transport failed')),
    });

    const { result } = renderHook(() => useLateralNav(), {
      wrapper: createWrapper(freshClient()),
    });

    await act(async () => {
      await result.current.navigateTo('Boom', 'artist');
    });

    expect(warnSpy).toHaveBeenCalledWith(
      '[detail] lateral nav fetch failed',
      expect.objectContaining({
        query: 'Boom',
        kind: 'artist',
        error: 'transport failed',
      }),
    );
    // The visible "not found" path stays silent; only real failures log.
    expect(result.current.state).toBe('idle');
  });

  it('does not log the ordinary "not found" result', async () => {
    mockResolveEntityQuery.mockReturnValue({
      queryKey: ['resolve-entity', 'artist', 'Nobody', 1],
      queryFn: () => Promise.resolve([]),
    });

    const { result } = renderHook(() => useLateralNav(), {
      wrapper: createWrapper(freshClient()),
    });

    await act(async () => {
      await result.current.navigateTo('Nobody', 'artist');
    });

    expect(warnSpy).not.toHaveBeenCalled();
    expect(result.current.error).toContain('not found');
  });
});
