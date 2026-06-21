/**
 * RelatedTracksSection — the "Related on SoundCloud" rail (related-tracks spec).
 *
 * The rail shows only for SoundCloud-sourced tracks with a non-empty set, and a
 * card tap navigates to that track's detail via the handoff push. expo-image,
 * expo-router, and the discovery client are mocked.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

jest.mock('expo-image', () => ({ Image: () => null }));

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
}));

const mockGetRelatedTracks = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getRelatedTracks: (...args: unknown[]) => mockGetRelatedTracks(...args),
}));

import { setDetailHandoff, getDetailHandoff } from '@shared/lib/detail-handoff';
import { RelatedTracksSection } from '../ui/RelatedTracksSection';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

function _track(title: string, provider = 'soundcloud', externalId = '1'): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'An Artist',
    image_url: null,
    confidence: 'low',
    sources: [{ provider, external_id: externalId, url: `https://${provider}/${externalId}` }],
    extras: {},
  };
}

function _ok(items: DiscoveryResult[]) {
  return { items, provider: 'soundcloud', status: 'ok', latency_ms: 1 };
}

function renderSection(result: DiscoveryResult): ReturnType<typeof render> {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }): ReactNode =>
    createElement(QueryClientProvider, { client: qc }, children);
  return render(
    createElement(RelatedTracksSection, { result, detailRoute: '/discover/detail' }),
    { wrapper },
  );
}

afterEach(() => {
  jest.clearAllMocks();
});

describe('RelatedTracksSection', () => {
  it('renders the rail with a card per related track for a SoundCloud-sourced track', async () => {
    mockGetRelatedTracks.mockResolvedValueOnce(
      _ok([_track('Fell In Love', 'soundcloud', '555'), _track('Collab Leak', 'soundcloud', '556')]),
    );
    const seed = _track('Seed', 'soundcloud', '12345');

    const { getByTestId } = renderSection(seed);

    await waitFor(() => expect(getByTestId('detail-related')).toBeTruthy());
    expect(getByTestId('detail-related-0')).toBeTruthy();
    expect(getByTestId('detail-related-1')).toBeTruthy();
  });

  it('renders nothing for a result with no SoundCloud source', () => {
    const seed = _track('Seed', 'deezer', '999');
    const { queryByTestId } = renderSection(seed);

    expect(mockGetRelatedTracks).not.toHaveBeenCalled();
    expect(queryByTestId('detail-related')).toBeNull();
  });

  it('renders nothing when the related set is empty', async () => {
    mockGetRelatedTracks.mockResolvedValueOnce(_ok([]));
    const seed = _track('Seed', 'soundcloud', '12345');

    const { queryByTestId } = renderSection(seed);

    await waitFor(() => expect(mockGetRelatedTracks).toHaveBeenCalled());
    expect(queryByTestId('detail-related')).toBeNull();
  });

  it('navigates to the related track detail when a card is tapped', async () => {
    mockGetRelatedTracks.mockResolvedValueOnce(_ok([_track('Fell In Love', 'soundcloud', '555')]));
    const seed = _track('Seed', 'soundcloud', '12345');

    const { getByTestId } = renderSection(seed);

    await waitFor(() => expect(getByTestId('detail-related-0')).toBeTruthy());
    fireEvent.press(getByTestId('detail-related-0'));

    expect(mockPush).toHaveBeenCalledWith('/discover/detail');
    expect(getDetailHandoff()?.title).toBe('Fell In Love');
  });
});
