import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { View } from 'react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import type { ActiveView } from '../activeView';
import { useSelection } from '../hooks/useSelection';
import { useTracksView } from '../hooks/useTracksView';
import { warmUpFirstRender } from '../../../../jest/warmUpFirstRender';

const mockGetTracks = jest.fn();
const mockGetAllTracks = jest.fn();

jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: unknown) => mockGetTracks(params),
  getAllTracks: (params: unknown) => mockGetAllTracks(params),
}));

const mockShowAlert = jest.fn();

jest.mock('@shared/ui/dialog/dialog', () => ({
  showAlert: (title: string, message: string) => mockShowAlert(title, message),
}));

const noop = () => undefined;

function WarmUpTracksScreen() {
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
  return view.error ? <View testID="library-error" /> : <>{view.content}</>;
}

warmUpFirstRender(async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockGetTracks.mockResolvedValue({ items: [], total: 0, limit: 0, offset: 0, has_more: false });
  render(
    <QueryClientProvider client={client}>
      <WarmUpTracksScreen />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(mockGetTracks).toHaveBeenCalled());
});

describe('a library list whose next page fails to load', () => {
  const PAGE = 5;
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

describe('playing a row from a library larger than the whole-library cap', () => {
  const CAP = 10_000;
  const track = (id: string) =>
    ({
      id: asTrackId(id),
      title: `Title ${id}`,
      artist: 'An Artist',
      album: null,
      duration_seconds: 180,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'ready',
      artwork_url: null,
    }) as unknown as TrackResponse;

  it('starts the tapped track when the capped whole-library fetch omits it', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const playFromList = jest.fn();
    const loaded = ['a', 'b', 'tapped'].map(track);
    mockGetTracks.mockReset().mockResolvedValue({
      items: loaded,
      total: CAP + 5,
      limit: 3,
      offset: 0,
      has_more: false,
    });
    mockGetAllTracks
      .mockReset()
      .mockResolvedValue(Array.from({ length: CAP }, (_, i) => track(`cap${i}`)));
    function Screen() {
      const selection = useSelection();
      const { view } = useTracksView({
        query: '',
        sort: 'recent',
        isActive: true,
        selection,
        queue: { playFromList } as never,
        playback: {} as never,
        retryMutation: { isInFlight: () => false } as never,
        onTrackPress: noop,
        onTrackMore: noop,
      });
      return <>{view.content}</>;
    }
    render(
      <QueryClientProvider client={client}>
        <Screen />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getByTestId('library-row-tapped')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-row-tapped'));

    await waitFor(() => expect(playFromList).toHaveBeenCalled());
    const [playable, startIndex] = playFromList.mock.calls[0] as [
      { source: { trackId: string } }[],
      number,
    ];
    expect(playable[startIndex]?.source.trackId).toBe('tapped');
    client.clear();
  });
});

describe('whole-library play and shuffle', () => {
  const track = (id: string) =>
    ({
      id: asTrackId(id),
      title: `Title ${id}`,
      artist: 'An Artist',
      album: null,
      duration_seconds: 180,
      added_at: '2026-01-01T00:00:00Z',
      acquisition_status: 'ready',
      artwork_url: null,
    }) as unknown as TrackResponse;

  function renderLibrary(queue: unknown) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    mockGetTracks.mockReset().mockResolvedValue({
      items: ['a', 'b'].map(track),
      total: 5,
      limit: 2,
      offset: 0,
      has_more: true,
    });
    function Screen() {
      const selection = useSelection();
      const { view } = useTracksView({
        query: '',
        sort: 'recent',
        isActive: true,
        selection,
        queue: queue as never,
        playback: {} as never,
        retryMutation: { isInFlight: () => false } as never,
        onTrackPress: noop,
        onTrackMore: noop,
      });
      return <>{view.content}</>;
    }
    render(
      <QueryClientProvider client={client}>
        <Screen />
      </QueryClientProvider>,
    );
    return client;
  }

  function pendingWholeLibrary(): (tracks: TrackResponse[]) => void {
    let finish: (tracks: TrackResponse[]) => void = noop;
    mockGetAllTracks.mockReset().mockReturnValue(
      new Promise<TrackResponse[]>((resolve) => {
        finish = resolve;
      }),
    );
    return (tracks) => finish(tracks);
  }

  it('tells the user only the loaded tracks play when the whole-library fetch fails', async () => {
    const shuffleFromList = jest.fn();
    const warn = jest.spyOn(console, 'warn').mockImplementation(noop);
    mockGetAllTracks.mockReset().mockRejectedValue(new Error('offline'));
    mockShowAlert.mockClear();
    const client = renderLibrary({ shuffleFromList });
    await waitFor(() => expect(screen.getByTestId('library-shuffle-all')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-shuffle-all'));

    await waitFor(() => expect(shuffleFromList).toHaveBeenCalledTimes(1));
    expect(mockShowAlert).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining('Playing the 2 tracks loaded so far'),
    );
    warn.mockRestore();
    client.clear();
  });

  it('shows a busy control and shuffles once when Shuffle All is tapped twice', async () => {
    const shuffleFromList = jest.fn();
    const finish = pendingWholeLibrary();
    const client = renderLibrary({ shuffleFromList });
    await waitFor(() => expect(screen.getByTestId('library-shuffle-all')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-shuffle-all'));
    fireEvent.press(screen.getByTestId('library-shuffle-all'));
    await waitFor(() => expect(screen.getByTestId('library-shuffle-all-busy')).toBeTruthy());
    finish(['a', 'b', 'c'].map(track));

    await waitFor(() => expect(shuffleFromList).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByTestId('library-shuffle-all-busy')).toBeNull());
    expect(mockGetAllTracks).toHaveBeenCalledTimes(1);
    client.clear();
  });

  it('plays once when a row is tapped twice while the whole library loads', async () => {
    const playFromList = jest.fn();
    const finish = pendingWholeLibrary();
    const client = renderLibrary({ playFromList });
    await waitFor(() => expect(screen.getByTestId('library-row-a')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-row-a'));
    fireEvent.press(screen.getByTestId('library-row-a'));
    finish(['a', 'b', 'c'].map(track));

    await waitFor(() => expect(playFromList).toHaveBeenCalledTimes(1));
    expect(mockGetAllTracks).toHaveBeenCalledTimes(1);
    client.clear();
  });
});
