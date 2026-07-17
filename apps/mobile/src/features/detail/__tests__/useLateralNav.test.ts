/**
 * useLateralNav — search-and-navigate for lateral browsing (AC#11-13).
 */

import { renderHook, act, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import type { ReactNode } from 'react';

import { useLateralNav } from '../hooks/useLateralNav';

// The lookup now routes through the shared resolve-entity query cache, so the
// hook needs a QueryClient. Fresh per render so no results leak across tests.
function renderLateralNav(): ReturnType<typeof renderHook<ReturnType<typeof useLateralNav>, void>> {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }): ReactNode =>
    createElement(QueryClientProvider, { client: qc }, children);
  return renderHook(() => useLateralNav(), { wrapper });
}

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useSegments: () => ['(tabs)', 'discover', 'detail'],
}));

const mockSearchDiscovery = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  searchDiscovery: (params: unknown) => mockSearchDiscovery(params),
}));

const mockSetDetailHandoff = jest.fn();
jest.mock('../../../shared/lib/detail-handoff', () => ({
  setDetailHandoff: (result: unknown) => mockSetDetailHandoff(result),
}));

afterEach(() => {
  jest.clearAllMocks();
});

describe('useLateralNav', () => {
  it('searches and navigates when result is found', async () => {
    const artistResult = {
      kind: 'artist',
      title: 'M83',
      subtitle: null,
      image_url: 'https://img.example/m83.jpg',
      confidence: 'high',
      sources: [{ provider: 'deezer', external_id: '123', url: 'https://deezer.com/artist/123' }],
      extras: {},
    };
    mockSearchDiscovery.mockResolvedValueOnce({ results: [artistResult] });

    const { result } = renderLateralNav();

    await act(async () => {
      await result.current.navigateTo('M83', 'artist');
    });

    expect(mockSearchDiscovery).toHaveBeenCalledWith({ q: 'M83', kinds: ['artist'], limit: 1, saveHistory: false });
    expect(mockSetDetailHandoff).toHaveBeenCalledWith(artistResult);
    expect(mockPush).toHaveBeenCalledWith('/discover/detail');
    expect(result.current.error).toBeNull();
  });

  it('sets error when no result found', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });

    const { result } = renderLateralNav();

    await act(async () => {
      await result.current.navigateTo('Unknown Artist', 'artist');
    });

    expect(result.current.error).toBe('Artist not found: "Unknown Artist"');
    expect(mockSetDetailHandoff).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it('sets Album label in error for album kind', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });

    const { result } = renderLateralNav();

    await act(async () => {
      await result.current.navigateTo('Unknown Album', 'album');
    });

    expect(result.current.error).toBe('Album not found: "Unknown Album"');
  });

  it('clears error on clearError call', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });

    const { result } = renderLateralNav();

    await act(async () => {
      await result.current.navigateTo('Unknown Artist', 'artist');
    });

    expect(result.current.error).not.toBeNull();

    act(() => {
      result.current.clearError();
    });

    expect(result.current.error).toBeNull();
  });

  it('clears error when starting new navigation', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });

    const { result } = renderLateralNav();

    await act(async () => {
      await result.current.navigateTo('Unknown Artist', 'artist');
    });

    expect(result.current.error).not.toBeNull();

    const artistResult = {
      kind: 'artist',
      title: 'M83',
      subtitle: null,
      image_url: 'https://img.example/m83.jpg',
      confidence: 'high',
      sources: [{ provider: 'deezer', external_id: '123', url: 'https://deezer.com/artist/123' }],
      extras: {},
    };
    mockSearchDiscovery.mockResolvedValueOnce({ results: [artistResult] });

    await act(async () => {
      await result.current.navigateTo('M83', 'artist');
    });

    expect(result.current.error).toBeNull();
  });

  it('tracks searching state during navigation', async () => {
    let resolveSearch: (value: { results: never[] }) => void;
    mockSearchDiscovery.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveSearch = resolve;
      }),
    );

    const { result } = renderLateralNav();

    expect(result.current.state).toBe('idle');

    let navPromise: Promise<void>;
    act(() => {
      navPromise = result.current.navigateTo('M83', 'artist');
    });

    await waitFor(() => {
      expect(result.current.state).toBe('searching');
    });

    await act(async () => {
      resolveSearch!({ results: [] });
      await navPromise!;
    });

    expect(result.current.state).toBe('idle');
  });

  it('ignores navigation attempts while already searching', async () => {
    let resolveSearch: (value: { results: never[] }) => void;
    mockSearchDiscovery.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveSearch = resolve;
      }),
    );

    const { result } = renderLateralNav();

    act(() => {
      void result.current.navigateTo('M83', 'artist');
    });

    await waitFor(() => {
      expect(result.current.state).toBe('searching');
    });

    // Attempt second navigation while first is in progress
    await act(async () => {
      await result.current.navigateTo('Another Artist', 'artist');
    });

    // Only one search should have been made
    expect(mockSearchDiscovery).toHaveBeenCalledTimes(1);

    // Clean up
    await act(async () => {
      resolveSearch!({ results: [] });
    });
  });
});
