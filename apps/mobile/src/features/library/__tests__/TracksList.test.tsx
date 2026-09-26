import { fireEvent, render, screen } from '@testing-library/react-native';

import type { ListRefresh } from '../refresh';
import { TracksList } from '../ui/TracksList';

function idleRefresh(): ListRefresh {
  return { onRefresh: jest.fn(), refreshing: false };
}

function renderEmptyTracksList(refresh: ListRefresh) {
  render(
    <TracksList
      tracks={[]}
      emptyLabel="No tracks yet"
      refresh={refresh}
      onPlay={jest.fn()}
      onPress={jest.fn()}
      onMore={jest.fn()}
      onRetry={jest.fn()}
      isRetrying={() => false}
      isPlaying={() => false}
    />,
  );
}

describe('library list shells — empty state', () => {
  it('shows the tracks empty label when there are no tracks', () => {
    renderEmptyTracksList(idleRefresh());

    expect(screen.getByText('No tracks yet')).toBeTruthy();
  });
});

describe('library list shells — pull to refresh', () => {
  it.each([['tracks', renderEmptyTracksList]])(
    'asks the %s list to refresh when the user pulls it down',
    (_name, renderList) => {
      const refresh = idleRefresh();
      renderList(refresh);

      fireEvent(screen.UNSAFE_getByProps({ refreshing: false }), 'refresh');

      expect(refresh.onRefresh).toHaveBeenCalledTimes(1);
    },
  );
});

const { Platform } = require('react-native');
const { asTrackId } = require('@shared/api-client/ids');

let mockWideWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWideWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

const oneTrack = [
  {
    id: asTrackId('t1'),
    title: 'Aerodynamic',
    artist: 'Daft Punk',
    album: 'Discovery',
    duration_seconds: 212,
    added_at: '2026-01-01T00:00:00Z',
    artwork_url: null,
    year: 2001,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    acquisition_status: 'ready',
    failure_reason: null,
  },
];

function renderTracksList(refresh: ListRefresh, tracks: unknown[]) {
  render(
    <TracksList
      tracks={tracks as never}
      emptyLabel="No tracks yet"
      refresh={refresh}
      onPlay={jest.fn()}
      onPress={jest.fn()}
      onMore={jest.fn()}
      onRetry={jest.fn()}
      isRetrying={() => false}
      isPlaying={() => false}
    />,
  );
}

describe('library list shells — wide web header', () => {
  const originalOS = Platform.OS;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWideWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWideWindowWidth = 390;
  });

  it('shows the column headers above a non-empty wide tracks list', () => {
    renderTracksList(idleRefresh(), oneTrack);

    expect(screen.getByTestId('library-wide-track-header')).toBeTruthy();
  });

  it('shows no column headers over an empty tracks list', () => {
    renderTracksList(idleRefresh(), []);

    expect(screen.queryByTestId('library-wide-track-header')).toBeNull();
  });

  it('shows no column headers on a compact width even on web', () => {
    mockWideWindowWidth = 390;
    renderTracksList(idleRefresh(), oneTrack);

    expect(screen.queryByTestId('library-wide-track-header')).toBeNull();
  });

  it('shows no column headers on native at a wide width', () => {
    Platform.OS = 'ios';
    renderTracksList(idleRefresh(), oneTrack);

    expect(screen.queryByTestId('library-wide-track-header')).toBeNull();
  });
});

function probeTrack(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id: asTrackId(id),
    title: `Title ${id}`,
    artist: `Artist ${id}`,
    album: `Album ${id}`,
    duration_seconds: 212,
    added_at: '2026-01-01T00:00:00Z',
    artwork_url: null,
    year: 2001,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    acquisition_status: 'ready',
    failure_reason: null,
    ...overrides,
  };
}

type ProbeListOverrides = {
  onPlay?: jest.Mock;
  onPress?: jest.Mock;
  onMore?: jest.Mock;
  paging?: {
    onEndReached: jest.Mock;
    isFetchingNextPage: boolean;
    nextPageFailed: boolean;
    onRetryNextPage: jest.Mock;
  };
  selection?: unknown;
};

function renderProbeTracksList(tracks: unknown[], overrides: ProbeListOverrides = {}) {
  return render(
    <TracksList
      tracks={tracks as never}
      emptyLabel="No tracks yet"
      refresh={idleRefresh()}
      onPlay={overrides.onPlay ?? jest.fn()}
      onPress={overrides.onPress ?? jest.fn()}
      onMore={overrides.onMore ?? jest.fn()}
      onRetry={jest.fn()}
      isRetrying={() => false}
      isPlaying={() => false}
      {...(overrides.paging ? { paging: overrides.paging } : {})}
      {...(overrides.selection ? { selection: overrides.selection as never } : {})}
    />,
  );
}

function probeSelection(active: boolean, selectedIds: string[] = []) {
  return {
    active,
    ids: selectedIds.map((id) => asTrackId(id)),
    count: selectedIds.length,
    has: (id: string) => selectedIds.includes(id),
    begin: jest.fn(),
    toggle: jest.fn(),
    selectAll: jest.fn(),
    clear: jest.fn(),
  };
}

describe('TracksList — wide web table, from the outside (#2842)', () => {
  const originalOS = Platform.OS;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWideWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWideWindowWidth = 390;
  });

  it('shows one header above many rows, not one per row', () => {
    renderProbeTracksList([probeTrack('a'), probeTrack('b'), probeTrack('c')]);

    expect(screen.getAllByTestId('library-wide-track-header')).toHaveLength(1);
  });

  it('plays the pressed row, not its neighbour, in the wide table', () => {
    const onPlay = jest.fn();
    renderProbeTracksList([probeTrack('a'), probeTrack('b')], { onPlay });

    fireEvent.press(screen.getByTestId('library-row-b'));

    expect(onPlay).toHaveBeenCalledTimes(1);
    expect(onPlay.mock.calls[0][0].id).toBe('b');
  });

  it('renders a wide row with no album and no duration without printing null or NaN', () => {
    renderProbeTracksList([probeTrack('a', { album: null, duration_seconds: null })]);

    expect(screen.getByText('Title a')).toBeTruthy();
    expect(screen.getByText('Artist a')).toBeTruthy();
    expect(screen.queryByText(/null|NaN|undefined/)).toBeNull();
  });

  it('shows the album and the m:ss duration in their own cells of a wide row', () => {
    renderProbeTracksList([probeTrack('a', { duration_seconds: 65 })]);

    expect(screen.getByText('Album a')).toBeTruthy();
    expect(screen.getByText('1:05')).toBeTruthy();
  });

  it('toggles a row instead of playing it while selecting in the wide table', () => {
    const onPlay = jest.fn();
    const selection = probeSelection(true);
    renderProbeTracksList([probeTrack('a'), probeTrack('b')], { onPlay, selection });

    fireEvent.press(screen.getByTestId('library-row-b'));

    expect(onPlay).not.toHaveBeenCalled();
    expect(selection.toggle).toHaveBeenCalledWith('b');
  });

  it('enters selection mode on a long press in the wide table', () => {
    const selection = probeSelection(false);
    renderProbeTracksList([probeTrack('a'), probeTrack('b')], { selection });

    fireEvent(screen.getByTestId('library-row-a'), 'longPress');

    expect(selection.begin).toHaveBeenCalledWith('a');
  });

  it('asks for the next page when the wide table scrolls to its end', () => {
    const paging = {
      onEndReached: jest.fn(),
      isFetchingNextPage: false,
      nextPageFailed: false,
      onRetryNextPage: jest.fn(),
    };
    renderProbeTracksList([probeTrack('a'), probeTrack('b')], { paging });

    fireEvent(screen.UNSAFE_getByProps({ refreshing: false }), 'endReached');

    expect(paging.onEndReached).toHaveBeenCalledTimes(1);
  });

  it('offers the load-more retry under the wide table when the next page failed', () => {
    const paging = {
      onEndReached: jest.fn(),
      isFetchingNextPage: false,
      nextPageFailed: true,
      onRetryNextPage: jest.fn(),
    };
    renderProbeTracksList([probeTrack('a')], { paging });

    fireEvent.press(screen.getByTestId('library-load-more-retry'));

    expect(paging.onRetryNextPage).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('library-wide-track-header')).toBeTruthy();
  });

  it('shows the loading-more footer under the wide table while the next page loads', () => {
    const paging = {
      onEndReached: jest.fn(),
      isFetchingNextPage: true,
      nextPageFailed: false,
      onRetryNextPage: jest.fn(),
    };
    renderProbeTracksList([probeTrack('a')], { paging });

    expect(screen.getByText('Loading more…')).toBeTruthy();
  });

  it('brings the header in once an empty wide list gets its first track', () => {
    const view = renderProbeTracksList([]);
    expect(screen.queryByTestId('library-wide-track-header')).toBeNull();

    view.rerender(
      <TracksList
        tracks={[probeTrack('a')] as never}
        emptyLabel="No tracks yet"
        refresh={idleRefresh()}
        onPlay={jest.fn()}
        onPress={jest.fn()}
        onMore={jest.fn()}
        onRetry={jest.fn()}
        isRetrying={() => false}
        isPlaying={() => false}
      />,
    );

    expect(screen.getByTestId('library-wide-track-header')).toBeTruthy();
  });

  it('shows the table headers from exactly 1000px on web', () => {
    mockWideWindowWidth = 1000;
    renderProbeTracksList([probeTrack('a')]);

    expect(screen.getByTestId('library-wide-track-header')).toBeTruthy();
  });

  it('keeps the compact list with no headers at 999px on web', () => {
    mockWideWindowWidth = 999;
    renderProbeTracksList([probeTrack('a')]);

    expect(screen.queryByTestId('library-wide-track-header')).toBeNull();
    expect(screen.getByText('Artist a · Album a')).toBeTruthy();
  });

  it('keeps the compact row on native at 999pt', () => {
    Platform.OS = 'ios';
    mockWideWindowWidth = 999;
    renderProbeTracksList([probeTrack('a')]);

    expect(screen.queryByTestId('library-wide-track-header')).toBeNull();
    expect(screen.getByText('Artist a · Album a')).toBeTruthy();
  });

  it('keeps the compact row on native at 1440pt', () => {
    Platform.OS = 'android';
    renderProbeTracksList([probeTrack('a')]);

    expect(screen.getByText('Artist a · Album a')).toBeTruthy();
  });

  it('keeps the compact row at 360px on web', () => {
    mockWideWindowWidth = 360;
    renderProbeTracksList([probeTrack('a')]);

    expect(screen.getByText('Artist a · Album a')).toBeTruthy();
  });
});
