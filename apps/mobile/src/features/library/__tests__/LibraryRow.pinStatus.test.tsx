import { render, screen } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';

import { LibraryRow } from '../ui/LibraryRow';

const ID = asTrackId('track-1');

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
  return OFFLINE_IDS.filter((id) => screen.UNSAFE_queryAllByProps({ 'data-testid': id }).length > 0);
}

const rowLabel = () => screen.getByTestId(`library-row-${ID}`).props.accessibilityLabel as string;

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
