// A list whose next page fails keeps the rows it already has and offers a retry in the
// footer; only a failed first page is the whole-list error.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { View } from 'react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import type { ActiveView } from '../activeView';
import { useSelection } from '../hooks/useSelection';
import { useTracksView } from '../hooks/useTracksView';

const mockGetTracks = jest.fn();

jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: unknown) => mockGetTracks(params),
  getAllTracks: jest.fn(),
}));

const noop = () => undefined;

describe('a library list whose next page fails to load', () => {
  const PAGE = 50;
  const RETRY = 'library-load-more-retry';

  const track = (i: number) =>
    ({
      id: asTrackId(`t${i}`),
      title: `Track ${i}`,
      artist: 'An Artist',
      album: null,
      duration_seconds: 180,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'ready',
      artwork_url: null,
    }) as unknown as TrackResponse;

  const rows = <T,>(make: (i: number) => T): T[] => Array.from({ length: PAGE }, (_, i) => make(i));

  function Screen({ view }: { view: ActiveView }): ReactElement {
    return view.error ? <View testID="library-error" /> : <>{view.content}</>;
  }

  function TracksScreen(): ReactElement {
    const selection = useSelection();
    const { view } = useTracksView({
      query: '',
      sort: 'recent',
      isActive: true,
      selection,
      queue: {} as never,
      playback: {} as never,
      retryMutation: { isInFlight: () => false } as never,
      onTrackPress: noop,
      onTrackMore: noop,
    });
    return <Screen view={view} />;
  }

  let client: QueryClient;
  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    mockGetTracks.mockReset();
  });

  afterEach(() => client.clear());

  const failure = () => Promise.reject(new Error('page 2 down'));

  it.each([
    [
      'tracks',
      'library-tracks-list',
      'Track 0',
      TracksScreen,
      () => {
        mockGetTracks.mockResolvedValueOnce({
          items: rows(track),
          total: 200,
          limit: PAGE,
          offset: 0,
          has_more: true,
        });
        mockGetTracks.mockImplementationOnce(failure);
        mockGetTracks.mockResolvedValueOnce({
          items: [track(900)],
          total: 200,
          limit: PAGE,
          offset: PAGE,
          has_more: false,
        });
      },
    ],
  ])('keeps the loaded %s and offers a retry', async (_noun, listId, firstRow, Component, seed) => {
    seed();
    render(<Component />, { wrapper });
    await waitFor(() => expect(screen.getByText(firstRow)).toBeTruthy());

    fireEvent(screen.getByTestId(listId), 'endReached');

    await waitFor(() => expect(screen.getByTestId(RETRY)).toBeTruthy());
    expect(screen.queryByTestId('library-error')).toBeNull();
    expect(screen.getByText(firstRow)).toBeTruthy();

    fireEvent.press(screen.getByTestId(RETRY));

    await waitFor(() => expect(screen.queryByTestId(RETRY)).toBeNull());
    expect(screen.queryByTestId('library-error')).toBeNull();
  });
});
