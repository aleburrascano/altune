/**
 * LibraryRow — shows a pending marker while a saved track's audio is not yet
 * acquired (view-result-detail slice 17, AC#10).
 *
 * LibraryRow consumes the @shared/ui barrel, which transitively loads
 * Artwork -> expo-image; mock it so the row renders under jest.
 */

import { render } from '@testing-library/react-native';

import { LibraryRow } from '../ui/LibraryRow';
import type { AcquisitionStatus, TrackResponse } from '../../../shared/api-client/types';

jest.mock('expo-image', () => ({ Image: () => null }));

function _track(
  acquisitionStatus: AcquisitionStatus,
  failureReason: string | null = null,
): TrackResponse {
  return {
    id: 't1',
    title: 'Midnight City',
    artist: 'M83',
    album: null,
    duration_seconds: null,
    added_at: '2026-05-01T12:00:00Z',
    acquisition_status: acquisitionStatus,
    artwork_url: null,
    failure_reason: failureReason,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

const noop = (): void => {};

describe('LibraryRow', () => {
  it('shows the pending marker when acquisition_status is pending', () => {
    const { getByTestId } = render(<LibraryRow track={_track('pending')} onPress={noop} onMore={noop} />);
    expect(getByTestId('library-row-pending-t1')).toBeTruthy();
  });

  it('omits the pending marker for any other status', () => {
    const { queryByTestId } = render(<LibraryRow track={_track('ready')} onPress={noop} onMore={noop} />);
    expect(queryByTestId('library-row-pending-t1')).toBeNull();
  });

  it('shows error text when acquisition_status is failed', () => {
    const { getByTestId } = render(
      <LibraryRow track={_track('failed', 'no_match_found')} onPress={noop} onMore={noop} />,
    );
    const failedEl = getByTestId('library-row-failed-t1');
    expect(failedEl).toBeTruthy();
    expect(failedEl.props.children).toBe("Couldn't find this track");
  });

  it('shows fallback text when failed with no reason', () => {
    const { getByTestId } = render(<LibraryRow track={_track('failed')} onPress={noop} onMore={noop} />);
    const failedEl = getByTestId('library-row-failed-t1');
    expect(failedEl.props.children).toBe('Acquisition failed');
  });
});
