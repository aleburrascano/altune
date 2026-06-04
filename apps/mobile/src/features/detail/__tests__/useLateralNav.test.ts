/**
 * useLateralNav — search-and-navigate for lateral browsing (AC#11-13).
 */

import { renderHook, act, waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';

const mockReplace = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ replace: mockReplace }),
}));

const mockSearchDiscovery = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  searchDiscovery: (params: unknown) => mockSearchDiscovery(params),
}));

const mockSetDetailHandoff = jest.fn();
jest.mock('../../../shared/lib/detail-handoff', () => ({
  setDetailHandoff: (result: unknown) => mockSetDetailHandoff(result),
}));

jest.spyOn(Alert, 'alert');

import { useLateralNav } from '../hooks/useLateralNav';

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

    const { result } = renderHook(() => useLateralNav());

    await act(async () => {
      await result.current.navigateTo('M83', 'artist');
    });

    expect(mockSearchDiscovery).toHaveBeenCalledWith({ q: 'M83', kinds: ['artist'], limit: 1 });
    expect(mockSetDetailHandoff).toHaveBeenCalledWith(artistResult);
    expect(mockReplace).toHaveBeenCalledWith('/detail');
  });

  it('shows alert when no result found', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });

    const { result } = renderHook(() => useLateralNav());

    await act(async () => {
      await result.current.navigateTo('Unknown Artist', 'artist');
    });

    expect(Alert.alert).toHaveBeenCalledWith('Artist not found', 'Couldn\'t find "Unknown Artist".');
    expect(mockSetDetailHandoff).not.toHaveBeenCalled();
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it('shows Album label for album kind', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });

    const { result } = renderHook(() => useLateralNav());

    await act(async () => {
      await result.current.navigateTo('Unknown Album', 'album');
    });

    expect(Alert.alert).toHaveBeenCalledWith('Album not found', 'Couldn\'t find "Unknown Album".');
  });

  it('tracks searching state during navigation', async () => {
    let resolveSearch: (value: { results: never[] }) => void;
    mockSearchDiscovery.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveSearch = resolve;
      }),
    );

    const { result } = renderHook(() => useLateralNav());

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

    const { result } = renderHook(() => useLateralNav());

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
