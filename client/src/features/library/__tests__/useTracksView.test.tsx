import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  fireEvent,
  render,
  screen,
  waitFor,
  waitForElementToBeRemoved,
} from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { Alert, View } from 'react-native';
import { asTrackId } from '@shared/apiClient/ids';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import type { ActiveView } from '@features/library/domain/libraryView';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { track } from '@features/library/__tests__/trackMutationFixtures';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { useSelection } from '../hooks/useSelection';
import { useTracksView } from '../hooks/useTracksView';
import { warmUpFirstRender } from '../../../../jest/warmUpFirstRender';

const noop = () => undefined;

function spiedPort(tracks: TrackResponse[]) {
  const memory = createMemoryLibraryPort({ tracks });
  return {
    ...memory,
    getTracks: jest.fn(memory.getTracks),
    getAllTracks: jest.fn(memory.getAllTracks),
  };
}

const base = track('base');

function row(id: string): TrackResponse {
  return { ...base, id: asTrackId(id), title: `Track ${id}` };
}

function library(ids: string[]): TrackResponse[] {
  return ids.map(row);
}

type TracksScreenProps = { queue?: unknown };

function TracksScreen({ queue = {} }: TracksScreenProps): ReactElement {
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
  return <Screen view={view} />;
}

function Screen({ view }: { view: ActiveView }): ReactElement {
  return view.error ? <View testID="library-error" /> : <>{view.content}</>;
}

function renderTracks(port: LibraryPort, queue: unknown = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
      </QueryClientProvider>
    );
  }
  render(<TracksScreen queue={queue} />, { wrapper });
  return client;
}

warmUpFirstRender(async () => {
  const port = spiedPort([]);
  renderTracks(port);
  await waitFor(() => expect(port.getTracks).toHaveBeenCalled());
});

describe('a library list whose next page fails to load', () => {
  const RETRY = 'library-load-more-retry';

  it('keeps the loaded tracks and offers a retry', async () => {
    const PAGE = 5;
    const port = spiedPort(library(['t0', 't1', 't2', 't3', 't4', 't5']));
    const serve = port.getTracks.getMockImplementation()!;
    port.getTracks.mockImplementationOnce((params, signal) =>
      serve({ ...params, limit: PAGE }, signal),
    );
    const client = renderTracks(port);
    await waitFor(() => expect(screen.getByText('Track t0')).toBeTruthy());
    port.getTracks.mockRejectedValueOnce(new Error('page 2 down'));

    fireEvent(screen.getByTestId('library-tracks-list'), 'endReached');

    await waitFor(() => expect(screen.getByTestId(RETRY)).toBeTruthy());
    expect(screen.queryByTestId('library-error')).toBeNull();
    expect(screen.getByText('Track t0')).toBeTruthy();

    fireEvent.press(screen.getByTestId(RETRY));

    await waitForElementToBeRemoved(() => screen.queryByTestId(RETRY));
    expect(screen.queryByTestId('library-error')).toBeNull();
    expect(screen.getByText('Track t5')).toBeTruthy();
    client.clear();
  });
});

describe('playing a row from a library larger than the whole-library cap', () => {
  const CAP = 10_000;

  it('starts the tapped track when the capped whole-library fetch omits it', async () => {
    const playFromList = jest.fn();
    const port = spiedPort(library(['a', 'b', 'tapped']));
    port.getAllTracks.mockResolvedValue({
      items: Array.from({ length: CAP }, (_, i) => row(`cap${i}`)),
      truncated: true,
    });
    const client = renderTracks(port, { playFromList });
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
  function pendingWholeLibrary(port: ReturnType<typeof spiedPort>): () => void {
    let finish: () => void = noop;
    port.getAllTracks.mockImplementationOnce(
      (params) =>
        new Promise((resolve) => {
          finish = () =>
            resolve(
              createMemoryLibraryPort({ tracks: library(['a', 'b', 'c']) }).getAllTracks(params),
            );
        }),
    );
    return () => finish();
  }

  it('tells the user only the loaded tracks play when the whole-library fetch fails', async () => {
    const shuffleFromList = jest.fn();
    const warn = jest.spyOn(console, 'warn').mockImplementation(noop);
    const alert = jest.spyOn(Alert, 'alert').mockImplementation(noop);
    const port = spiedPort(library(['a', 'b']));
    port.getAllTracks.mockRejectedValue(new Error('offline'));
    const client = renderTracks(port, { shuffleFromList });
    await waitFor(() => expect(screen.getByTestId('library-shuffle-all')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-shuffle-all'));

    await waitFor(() => expect(shuffleFromList).toHaveBeenCalledTimes(1));
    expect(alert).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining('Playing the 2 tracks loaded so far'),
    );
    const [shuffled] = shuffleFromList.mock.calls[0] as [{ source: { trackId: string } }[]];
    expect(shuffled.map((t) => t.source.trackId).sort()).toEqual(['a', 'b']);
    alert.mockRestore();
    warn.mockRestore();
    client.clear();
  });

  it('shows a busy control and shuffles once when Shuffle All is tapped twice', async () => {
    const shuffleFromList = jest.fn();
    const port = spiedPort(library(['a', 'b']));
    const finish = pendingWholeLibrary(port);
    const client = renderTracks(port, { shuffleFromList });
    await waitFor(() => expect(screen.getByTestId('library-shuffle-all')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-shuffle-all'));
    fireEvent.press(screen.getByTestId('library-shuffle-all'));
    await waitFor(() => expect(screen.getByTestId('library-shuffle-all-busy')).toBeTruthy());
    finish();

    await waitFor(() => expect(shuffleFromList).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByTestId('library-shuffle-all-busy')).toBeNull());
    expect(port.getAllTracks).toHaveBeenCalledTimes(1);
    client.clear();
  });

  it('plays once when a row is tapped twice while the whole library loads', async () => {
    const playFromList = jest.fn();
    const port = spiedPort(library(['a', 'b']));
    const finish = pendingWholeLibrary(port);
    const client = renderTracks(port, { playFromList });
    await waitFor(() => expect(screen.getByTestId('library-row-a')).toBeTruthy());

    fireEvent.press(screen.getByTestId('library-row-a'));
    fireEvent.press(screen.getByTestId('library-row-a'));
    finish();

    await waitFor(() => expect(playFromList).toHaveBeenCalledTimes(1));
    expect(port.getAllTracks).toHaveBeenCalledTimes(1);
    client.clear();
  });
});
