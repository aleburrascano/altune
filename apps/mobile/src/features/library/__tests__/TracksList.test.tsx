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
