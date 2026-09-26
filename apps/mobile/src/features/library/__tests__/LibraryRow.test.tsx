import { Platform } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';

import { LibraryRow } from '../ui/LibraryRow';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

const ID = asTrackId('track-1');

const fields = {
  id: ID,
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
};

const readyTrack = {
  ...fields,
  acquisition_status: 'ready',
  failure_reason: null,
} as TrackResponse;
const pendingTrack = {
  ...fields,
  acquisition_status: 'pending',
  failure_reason: null,
} as TrackResponse;
const failedTrack = {
  ...fields,
  acquisition_status: 'failed',
  failure_reason: 'no_source',
  failure_message: 'No source found',
} as TrackResponse;
const albumlessTrack = { ...readyTrack, album: null } as TrackResponse;

const rowLabel = () => screen.getByTestId(`library-row-${ID}`).props.accessibilityLabel as string;
const rowRole = () => screen.getByTestId(`library-row-${ID}`).props.accessibilityRole as string;
// Pressable always forwards an accessibilityState object, so `checked` — not the
// object — is what says whether the row is a checkbox.
const rowChecked = () =>
  (screen.getByTestId(`library-row-${ID}`).props.accessibilityState as { checked?: boolean })
    .checked;

// The offline pin indicator tests write pins into the store; start every test
// with none so a pin never leaks into another group's label.
beforeEach(() => {
  usePinnedStore.setState({ entries: {} });
});

describe('LibraryRow — selection mode', () => {
  it('toggles the selection instead of playing or opening the track', () => {
    const onToggle = jest.fn();
    const onPlay = jest.fn();
    const onPress = jest.fn();
    render(
      <LibraryRow
        track={readyTrack}
        onPlay={onPlay}
        onPress={onPress}
        onMore={jest.fn()}
        selectable={{ selected: false, onToggle }}
      />,
    );

    fireEvent.press(screen.getByTestId(`library-row-${ID}`));

    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onPlay).not.toHaveBeenCalled();
    expect(onPress).not.toHaveBeenCalled();
  });

  it('announces a selected row as a checked checkbox', () => {
    render(
      <LibraryRow
        track={readyTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        selectable={{ selected: true, onToggle: jest.fn() }}
      />,
    );

    expect(rowRole()).toBe('checkbox');
    expect(rowChecked()).toBe(true);
    expect(screen.getByTestId(`library-row-check-${ID}`)).toBeTruthy();
  });

  it('replaces the duration and more-options button with the checkbox', () => {
    render(
      <LibraryRow
        track={readyTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        selectable={{ selected: false, onToggle: jest.fn() }}
      />,
    );

    expect(screen.getByTestId(`library-row-check-${ID}`)).toBeTruthy();
    expect(screen.queryByTestId(`library-row-more-${ID}`)).toBeNull();
    expect(screen.queryByText('3:32')).toBeNull();
  });
});

describe('LibraryRow — playback mode', () => {
  it('plays a ready track on press and announces itself as a button', () => {
    const onPlay = jest.fn();
    const onPress = jest.fn();
    render(<LibraryRow track={readyTrack} onPlay={onPlay} onPress={onPress} onMore={jest.fn()} />);

    fireEvent.press(screen.getByTestId(`library-row-${ID}`));

    expect(onPlay).toHaveBeenCalledTimes(1);
    expect(onPress).not.toHaveBeenCalled();
    expect(rowRole()).toBe('button');
    expect(rowChecked()).toBeUndefined();
  });

  it('opens a track that is not ready instead of playing it', () => {
    const onPlay = jest.fn();
    const onPress = jest.fn();
    render(
      <LibraryRow track={pendingTrack} onPlay={onPlay} onPress={onPress} onMore={jest.fn()} />,
    );

    fireEvent.press(screen.getByTestId(`library-row-${ID}`));

    expect(onPress).toHaveBeenCalledTimes(1);
    expect(onPlay).not.toHaveBeenCalled();
  });

  it('shows the duration alongside a labelled more-options button', () => {
    render(<LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(screen.getByText('3:32')).toBeTruthy();
    expect(screen.getByTestId(`library-row-more-${ID}`).props.accessibilityLabel).toBe(
      'More options for Aerodynamic',
    );
  });

  it('long-presses only when the caller supplies a handler', () => {
    const onLongPress = jest.fn();
    render(
      <LibraryRow
        track={readyTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        onLongPress={onLongPress}
      />,
    );

    fireEvent(screen.getByTestId(`library-row-${ID}`), 'longPress');

    expect(onLongPress).toHaveBeenCalledTimes(1);
  });
});

describe('LibraryRow — acquisition failure', () => {
  it('offers a retry that fires the retry callback', () => {
    const onRetry = jest.fn();
    render(
      <LibraryRow track={failedTrack} onPress={jest.fn()} onMore={jest.fn()} onRetry={onRetry} />,
    );

    fireEvent.press(screen.getByTestId(`library-row-retry-${ID}`));

    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId(`library-row-failed-${ID}`).props.children).toBe('No source found');
  });

  it('replaces the retry action with a spinner while retrying', () => {
    render(
      <LibraryRow
        track={failedTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        onRetry={jest.fn()}
        retrying
      />,
    );

    expect(screen.getByTestId(`library-row-retrying-${ID}`)).toBeTruthy();
    expect(screen.queryByTestId(`library-row-retry-${ID}`)).toBeNull();
    expect(screen.getByTestId(`library-row-failed-${ID}`).props.children).toBe('Retrying…');
  });

  it('states the failure with no retry action when retrying is not offered', () => {
    render(<LibraryRow track={failedTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(screen.getByTestId(`library-row-failed-${ID}`).props.children).toBe('No source found');
    expect(screen.queryByTestId(`library-row-retry-${ID}`)).toBeNull();
    expect(screen.queryByTestId(`library-row-retrying-${ID}`)).toBeNull();
  });
});

describe('LibraryRow — accessibility label', () => {
  it('names the track, artist and album of a ready track', () => {
    render(<LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(rowLabel()).toBe('Aerodynamic by Daft Punk · Discovery');
  });

  it('drops the album segment when the track has no album', () => {
    render(<LibraryRow track={albumlessTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(rowLabel()).toBe('Aerodynamic by Daft Punk');
  });

  it('announces a pending acquisition', () => {
    render(<LibraryRow track={pendingTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(rowLabel()).toBe('Aerodynamic by Daft Punk · Discovery, pending');
  });

  it('announces a failed acquisition whose retry is available', () => {
    render(
      <LibraryRow track={failedTrack} onPress={jest.fn()} onMore={jest.fn()} onRetry={jest.fn()} />,
    );

    expect(rowLabel()).toBe('Aerodynamic by Daft Punk · Discovery, failed, retry available');
  });

  it('announces a retry in flight rather than one available', () => {
    render(
      <LibraryRow
        track={failedTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        onRetry={jest.fn()}
        retrying
      />,
    );

    expect(rowLabel()).toBe('Aerodynamic by Daft Punk · Discovery, failed, retrying');
  });

  it('carries the same label in selection mode as in playback mode', () => {
    render(<LibraryRow track={failedTrack} onPress={jest.fn()} onMore={jest.fn()} />);
    const playbackLabel = rowLabel();
    screen.unmount();

    render(
      <LibraryRow
        track={failedTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        selectable={{ selected: false, onToggle: jest.fn() }}
      />,
    );

    expect(rowLabel()).toBe(playbackLabel);
  });
});

const track = {
  id: ID,
  title: 'Aerodynamic',
  artist: 'Daft Punk',
  album: 'Discovery',
  duration_seconds: 212,
  added_at: '2026-01-01T00:00:00Z',
  acquisition_status: 'ready',
  artwork_url: null,
  failure_reason: null,
  year: 2001,
  genre: null,
  track_number: null,
  album_artist: null,
  isrc: null,
  audio_ref: null,
} as TrackResponse;

function renderWithPin(entry: PinnedEntry | undefined) {
  usePinnedStore.setState({ entries: entry ? { [ID]: entry } : {} });
  render(<LibraryRow track={track} onPress={jest.fn()} onMore={jest.fn()} />);
}

const OFFLINE_IDS = [
  `library-row-offline-${ID}`,
  `library-row-offline-pending-${ID}`,
  `library-row-offline-failed-${ID}`,
];

// Lucide icons forward testID as `data-testid` onto the (mocked) svg root.
function shownOfflineIds(): string[] {
  return OFFLINE_IDS.filter(
    (id) => screen.UNSAFE_queryAllByProps({ 'data-testid': id }).length > 0,
  );
}

describe('LibraryRow — offline pin indicator', () => {
  it('marks a failed pin with its own indicator and label, distinct from never-pinned', () => {
    renderWithPin({ trackId: ID, status: 'failed' });
    expect(shownOfflineIds()).toEqual([`library-row-offline-failed-${ID}`]);
    expect(rowLabel()).toMatch(/, download failed$/);
  });

  it('shows no indicator for a track that was never pinned', () => {
    renderWithPin(undefined);
    expect(shownOfflineIds()).toEqual([]);
    expect(rowLabel()).not.toMatch(/download/);
  });

  it('keeps the ready and pending indicators for their statuses', () => {
    renderWithPin({ trackId: ID, status: 'ready', uri: 'file:///a' });
    expect(shownOfflineIds()).toEqual([`library-row-offline-${ID}`]);
    screen.unmount();
    renderWithPin({ trackId: ID, status: 'queued' });
    expect(shownOfflineIds()).toEqual([`library-row-offline-pending-${ID}`]);
  });
});

describe('LibraryRow — wide web layout', () => {
  const originalOS = Platform.OS;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWindowWidth = 390;
  });

  it('splits the artist and album into their own columns instead of one subtitle', () => {
    render(<LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(screen.getByText('Daft Punk')).toBeTruthy();
    expect(screen.getByText('Discovery')).toBeTruthy();
    expect(screen.queryByText('Daft Punk · Discovery')).toBeNull();
  });

  it('plays a ready track when the row is pressed, same as the compact row', () => {
    const onPlay = jest.fn();
    render(<LibraryRow track={readyTrack} onPlay={onPlay} onPress={jest.fn()} onMore={jest.fn()} />);

    fireEvent.press(screen.getByTestId(`library-row-${ID}`));

    expect(onPlay).toHaveBeenCalledTimes(1);
  });

  it('keeps the row a keyboard-focusable button with the more-options button still reachable', () => {
    render(<LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(rowRole()).toBe('button');
    expect(screen.getByTestId(`library-row-more-${ID}`)).toBeTruthy();
  });

  it('falls back to the compact row in selection mode even at a wide width', () => {
    render(
      <LibraryRow
        track={readyTrack}
        onPress={jest.fn()}
        onMore={jest.fn()}
        selectable={{ selected: false, onToggle: jest.fn() }}
      />,
    );

    expect(screen.getByText('Daft Punk · Discovery')).toBeTruthy();
  });
});

describe('LibraryRow — wide web row keeps the compact row behaviours (#2842)', () => {
  const originalOS = Platform.OS;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWindowWidth = 390;
  });

  it('opens a track that is not ready instead of playing it', () => {
    const onPlay = jest.fn();
    const onPress = jest.fn();
    render(<LibraryRow track={pendingTrack} onPlay={onPlay} onPress={onPress} onMore={jest.fn()} />);

    fireEvent.press(screen.getByTestId(`library-row-${ID}`));

    expect(onPress).toHaveBeenCalledTimes(1);
    expect(onPlay).not.toHaveBeenCalled();
  });

  it('offers the retry on a failed track in its status cell', () => {
    const onRetry = jest.fn();
    render(<LibraryRow track={failedTrack} onPress={jest.fn()} onMore={jest.fn()} onRetry={onRetry} />);

    fireEvent.press(screen.getByTestId(`library-row-retry-${ID}`));

    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('shows the duration as m:ss in its own cell', () => {
    render(<LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(screen.getByText('3:32')).toBeTruthy();
  });

  it('shows no album text for a track with no album', () => {
    render(<LibraryRow track={albumlessTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(screen.getByText('Daft Punk')).toBeTruthy();
    expect(screen.queryByText('Discovery')).toBeNull();
    expect(screen.queryByText(/null|undefined/)).toBeNull();
  });

  it('long-presses into selection when the caller supplies a handler', () => {
    const onLongPress = jest.fn();
    render(
      <LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} onLongPress={onLongPress} />,
    );

    fireEvent(screen.getByTestId(`library-row-${ID}`), 'longPress');

    expect(onLongPress).toHaveBeenCalledTimes(1);
  });

  it('labels the more-options button with the track title', () => {
    render(<LibraryRow track={readyTrack} onPress={jest.fn()} onMore={jest.fn()} />);

    expect(screen.getByTestId(`library-row-more-${ID}`).props.accessibilityLabel).toBe(
      'More options for Aerodynamic',
    );
  });
});
