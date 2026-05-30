/**
 * LibraryRow — shows a pending marker while a saved track's audio is not yet
 * acquired (view-result-detail slice 17, AC#10).
 *
 * LibraryRow consumes the @shared/ui barrel, which transitively loads
 * Artwork -> expo-image; mock it so the row renders under jest.
 */

import { render } from '@testing-library/react-native';

jest.mock('expo-image', () => ({ Image: () => null }));

import { LibraryRow } from '../ui/LibraryRow';
import type { TrackResponse } from '../../../shared/api-client/types';

function _track(acquisitionStatus: string): TrackResponse {
  return {
    id: 't1',
    title: 'Midnight City',
    artist: 'M83',
    album: null,
    duration_seconds: null,
    added_at: '2026-05-01T12:00:00Z',
    acquisition_status: acquisitionStatus,
    artwork_url: null,
  };
}

describe('LibraryRow', () => {
  it('shows the pending marker when acquisition_status is pending', () => {
    const { getByTestId } = render(<LibraryRow track={_track('pending')} />);
    expect(getByTestId('library-row-pending-t1')).toBeTruthy();
  });

  it('omits the pending marker for any other status', () => {
    const { queryByTestId } = render(<LibraryRow track={_track('owned')} />);
    expect(queryByTestId('library-row-pending-t1')).toBeNull();
  });
});
