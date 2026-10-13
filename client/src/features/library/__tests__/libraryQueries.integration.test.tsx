import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { signInTestUser } from '@testing/signInTestUser';
import { useLibraryAlbums, useLibraryArtists } from '@features/library/hooks/useLibraryGroups';
import { useLibraryTracks } from '@features/library/hooks/useLibraryTracks';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

beforeEach(async () => {
  await signInTestUser();
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
    await waitFor(() => expect(requestFor('da')?.signal.aborted).toBe(false));

    rerender({ q: 'daft' });
    await waitFor(() => expect(requestFor('daft')?.signal.aborted).toBe(false));

    expect(requestFor('da')?.signal.aborted).toBe(true);
    expect(requestFor('daft')?.signal.aborted).toBe(false);
    unmount();
  });
});
