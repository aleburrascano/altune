// #1704: a failed tracks/albums/artists load is the library screen's primary failure
// mode, and the screen answers every one of them with the same generic copy. Without a
// line here the report "my library won't load" reaches triage with no chip, no status
// and no failure class. Redacted like #1703: the caught error and the search term stay
// out of the log.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { ApiError, NetworkError } from '@shared/errors';

import { useLibraryAlbums } from '../hooks/useLibraryAlbums';
import { useLibraryArtists } from '../hooks/useLibraryArtists';
import { useLibraryTracks } from '../hooks/useLibraryTracks';
import { usePlaylistActions } from '../hooks/usePlaylistActions';
import { usePlaylistsView } from '../hooks/usePlaylistsView';

const mockGetTracks = jest.fn();
const mockGetLibraryAlbums = jest.fn();
const mockGetLibraryArtists = jest.fn();
const mockGetPlaylists = jest.fn();

jest.mock('@shared/api-client/tracks', () => ({
  getTracks: () => mockGetTracks(),
  getAllTracks: jest.fn(),
}));
jest.mock('@shared/api-client/library', () => ({
  getLibraryAlbums: () => mockGetLibraryAlbums(),
  getLibraryArtists: () => mockGetLibraryArtists(),
}));

jest.mock('@shared/api-client/playlists', () => ({
  getPlaylists: () => mockGetPlaylists(),
}));

const SECRET = 'daft punk discovery bootleg';
const emptyTracksPage = { items: [], total: 0, limit: 200, offset: 0, has_more: false };
const emptyGroups = { items: [], total: 0 };

let client: QueryClient;
let warnSpy: jest.SpyInstance;

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

/**
 * What a console would render. `message` and `stack` are non-enumerable, so a plain
 * `JSON.stringify` of a logged Error yields `{}` and would hide the leak this file
 * exists to catch.
 */
function loggedText(): string {
  return JSON.stringify(warnSpy.mock.calls, (_key, value: unknown) =>
    value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value,
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  mockGetTracks.mockReset();
  mockGetLibraryAlbums.mockReset();
  mockGetLibraryArtists.mockReset();
  mockGetPlaylists.mockReset();
});

afterEach(() => {
  warnSpy.mockRestore();
  client.clear();
});

describe.each([
  ['tracks', useLibraryTracks, () => mockGetTracks, emptyTracksPage],
  ['albums', useLibraryAlbums, () => mockGetLibraryAlbums, emptyGroups],
  ['artists', useLibraryArtists, () => mockGetLibraryArtists, emptyGroups],
] as const)('a failed useLibrary%s load', (chip, useHook, fetcher, emptyResponse) => {
  it('logs the chip, its sort, the status and the failure class', async () => {
    fetcher().mockRejectedValue(new ApiError(503, SECRET, 'library_unavailable', 'corr-1'));
    const { result } = renderHook(() => useHook('', 'az', true), { wrapper });

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
    fetcher().mockRejectedValue(new NetworkError('transport', `API ${SECRET} is unreachable`));
    const { result } = renderHook(() => useHook(SECRET, 'recent', true), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());

    expect(loggedText()).not.toContain(SECRET);
    expect(warnSpy).toHaveBeenCalledWith(`[library] ${chip} query failed`, {
      sort: 'recent',
      isSearching: true,
      failure: 'network',
    });
  });

  it('logs nothing while the query keeps answering', async () => {
    fetcher().mockResolvedValue(emptyResponse);
    const { result } = renderHook(() => useHook('', 'recent', true), { wrapper });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(warnSpy).not.toHaveBeenCalled();
  });
});

describe('a failed usePlaylistActions load', () => {
  it('logs the chip and surfaces the error on the playlists view', async () => {
    mockGetPlaylists.mockRejectedValue(new ApiError(500, SECRET, 'internal', 'corr-2'));
    const { result } = renderHook(
      () => {
        const pl = usePlaylistActions();
        return usePlaylistsView({ pl, sort: 'az', onPlaylistPress: jest.fn() });
      },
      { wrapper },
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
