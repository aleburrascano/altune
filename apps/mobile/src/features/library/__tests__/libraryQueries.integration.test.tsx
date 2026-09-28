import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { useLibraryAlbums } from '../hooks/useLibraryAlbums';
import { useLibraryArtists } from '../hooks/useLibraryArtists';
import { useLibraryTracks } from '../hooks/useLibraryTracks';

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

type RecordedRequest = { query: string; signal: AbortSignal };

const requestFor = (q: string): RecordedRequest | undefined =>
  (__http.requests as RecordedRequest[]).find((r) => new URLSearchParams(r.query).get('q') === q);

describe.each([
  ['tracks', 'GET /v1/tracks', useLibraryTracks],
  ['albums', 'GET /v1/library/albums', useLibraryAlbums],
  ['artists', 'GET /v1/library/artists', useLibraryArtists],
] as const)('useLibrary %s, superseded search requests', (_name, spec, useHook) => {
  it('aborts the in-flight request for the old query once the query changes', async () => {
    __http.hang(spec);
    const { rerender, unmount } = renderHook(({ q }: { q: string }) => useHook(q, 'recent', true), {
      wrapper,
      initialProps: { q: 'da' },
    });
    await waitFor(() => expect(requestFor('da')).toBeDefined());

    rerender({ q: 'daft' });
    await waitFor(() => expect(requestFor('daft')).toBeDefined());

    expect(requestFor('da')?.signal.aborted).toBe(true);
    expect(requestFor('daft')?.signal.aborted).toBe(false);
    unmount();
  });
});
