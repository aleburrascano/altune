// #1667: leaving a detail screen must abort its in-flight library lookup instead of
// letting it run to the shared 15s deadline. TanStack only aborts on unmount when the
// queryFn consumed the context signal, so this drives the real api-client against the
// fetch double and checks the recorded request's signal.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { useLibraryAlbumsForArtist } from '../hooks/useLibraryAlbumsForArtist';
import { useLibraryTracksForAlbum, useLibraryTracksForArtist } from '../hooks/useLibraryTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => client.clear());

describe.each([
  [
    'useLibraryAlbumsForArtist',
    'GET /v1/library/albums',
    (): void => void useLibraryAlbumsForArtist('daft punk', true),
  ],
  [
    'useLibraryTracksForAlbum',
    'GET /v1/tracks',
    (): void => void useLibraryTracksForAlbum('discovery', 'daft punk'),
  ],
  [
    'useLibraryTracksForArtist',
    'GET /v1/tracks',
    (): void => void useLibraryTracksForArtist('daft punk'),
  ],
] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
  it('aborts the in-flight request when the screen unmounts', async () => {
    __http.hang(spec);
    const { unmount } = renderHook(useHook, { wrapper });
    await waitFor(() => expect(__http.last()).toBeDefined());
    const { signal } = __http.last() as { signal: AbortSignal };

    unmount();

    await waitFor(() => expect(signal.aborted).toBe(true));
  });
});
