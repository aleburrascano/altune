/**
 * useLyrics — detail-open Deezer lyrics fetch (docs/providers/deezer.md cap 6).
 * Real QueryClient; getLyrics mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useLyrics } from '../hooks/useLyrics';
import type { LyricsResponse } from '../../../shared/api-client/discovery';

const mockGet = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getLyrics: (...args: unknown[]) => mockGet(...args),
}));

function _lyrics(over: Partial<LyricsResponse> = {}): LyricsResponse {
  return {
    plain: "Hello, it's me",
    synced_lines: [
      { timecode: '[00:12.34]', line: "Hello, it's me", milliseconds: 12340, duration: 2000 },
    ],
    writers: ['Adele Laurie Blue Adkins'],
    copyright: 'Universal',
    ...over,
  };
}

const _empty: LyricsResponse = {
  plain: '',
  synced_lines: [],
  writers: [],
  copyright: '',
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGet.mockReset();
});

describe('useLyrics', () => {
  it('fetches and returns lyrics for a track', async () => {
    mockGet.mockResolvedValueOnce(_lyrics());

    const { result } = renderHook(
      () => useLyrics({ title: 'Hello', subtitle: 'Adele' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.lyrics).not.toBeNull());
    expect(result.current.lyrics?.synced_lines).toHaveLength(1);
    expect(mockGet).toHaveBeenCalledWith({ title: 'Hello', subtitle: 'Adele' });
  });

  it('returns lyrics when only plain text is present (no synced lines)', async () => {
    mockGet.mockResolvedValueOnce(_lyrics({ synced_lines: [] }));

    const { result } = renderHook(
      () => useLyrics({ title: 'Hello', subtitle: 'Adele' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.lyrics).not.toBeNull());
    expect(result.current.lyrics?.plain).toContain("Hello, it's me");
  });

  it('treats an empty payload as no lyrics', async () => {
    mockGet.mockResolvedValueOnce(_empty);

    const { result } = renderHook(
      () => useLyrics({ title: 'Instrumental', subtitle: 'Someone' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.lyrics).toBeNull();
  });

  it('does not fetch when disabled', () => {
    const { result } = renderHook(
      () => useLyrics({ title: 'Hello', subtitle: 'Adele', enabled: false }),
      { wrapper: _wrapper() },
    );

    expect(mockGet).not.toHaveBeenCalled();
    expect(result.current.lyrics).toBeNull();
  });

  it('surfaces isError without throwing when the request fails', async () => {
    mockGet.mockRejectedValueOnce(new Error('network'));

    const { result } = renderHook(
      () => useLyrics({ title: 'Hello', subtitle: 'Adele' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.lyrics).toBeNull();
  });
});
