import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { ApiError, NetworkError } from '@shared/failure/errors';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { useLibraryAlbums, useLibraryArtists } from '@features/library/hooks/useLibraryGroups';
import { useLibraryTracks } from '@features/library/hooks/useLibraryTracks';
import { usePlaylistActions } from '@features/library/hooks/usePlaylistActions';
import { usePlaylistsView } from '@features/library/hooks/usePlaylistsView';

const SECRET = 'daft punk discovery bootleg';

type FailingMethod = 'getTracks' | 'getLibraryAlbums' | 'getLibraryArtists';

let client: QueryClient;
let warnSpy: jest.SpyInstance;

function wrapperFor(port: LibraryPort) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
      </QueryClientProvider>
    );
  };
}

function failingPort(method: FailingMethod, err: Error): LibraryPort {
  return { ...createMemoryLibraryPort(), [method]: jest.fn(() => Promise.reject(err)) };
}

function loggedText(): string {
  return JSON.stringify(warnSpy.mock.calls, (_key, value: unknown) =>
    value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value,
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
  client.clear();
});

describe.each([
  ['tracks', useLibraryTracks, 'getTracks'],
  ['albums', useLibraryAlbums, 'getLibraryAlbums'],
  ['artists', useLibraryArtists, 'getLibraryArtists'],
] as const)('a failed useLibrary%s load', (chip, useHook, method) => {
  it('logs the chip, its sort, the status and the failure class', async () => {
    const port = failingPort(method, new ApiError(503, SECRET, 'library_unavailable', 'corr-1'));
    const { result } = renderHook(() => useHook('', 'az', true), { wrapper: wrapperFor(port) });

    await waitFor(() => expect(result.current.error).not.toBeNull());

    expect(warnSpy).toHaveBeenCalledWith(`[library] ${chip} query failed`, {
      sort: 'az',
      isSearching: false,
      status: 503,
      code: 'library_unavailable',
      failure: 'server',
      correlationId: 'corr-1',
    });
  });

  it('classifies an unreachable API as a network failure of a search', async () => {
    const port = failingPort(method, new NetworkError('transport', `API ${SECRET} is unreachable`));
    const { result } = renderHook(() => useHook(SECRET, 'recent', true), {
      wrapper: wrapperFor(port),
    });

    await waitFor(() => expect(result.current.error).not.toBeNull());

    expect(loggedText()).not.toContain(SECRET);
    expect(warnSpy).toHaveBeenCalledWith(`[library] ${chip} query failed`, {
      sort: 'recent',
      isSearching: true,
      failure: 'network',
    });
  });

  it('logs nothing while the query keeps answering', async () => {
    const { result } = renderHook(() => useHook('', 'recent', true), {
      wrapper: wrapperFor(createMemoryLibraryPort()),
    });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(warnSpy).not.toHaveBeenCalled();
  });
});

describe('a failed usePlaylistActions load', () => {
  it('logs the chip and surfaces the error on the playlists view', async () => {
    const memory = createMemoryLibraryPort();
    const port = {
      ...memory,
      getPlaylists: jest.fn(() => Promise.reject(new ApiError(500, SECRET, 'internal', 'corr-2'))),
    };
    const { result } = renderHook(
      () => {
        const pl = usePlaylistActions();
        return usePlaylistsView({ pl, sort: 'az', onPlaylistPress: jest.fn() });
      },
      { wrapper: wrapperFor(port) },
    );

    await waitFor(() => expect(result.current.view.error).not.toBeNull());

    expect(loggedText()).not.toContain(SECRET);
    expect(warnSpy).toHaveBeenCalledWith('[library] playlists query failed', {
      isSearching: false,
      status: 500,
      code: 'internal',
      failure: 'server',
      correlationId: 'corr-2',
    });
  });
});
