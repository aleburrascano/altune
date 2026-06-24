/**
 * LibraryRow retry behavior — loading state, a11y, and callback wiring.
 *
 * Companion to LibraryRow.test.tsx (which covers pending/failed rendering).
 * These tests cover the `retrying` prop and `onRetry` callback added by
 * the UX resilience fix.
 */

import { fireEvent, render } from '@testing-library/react-native';

import { LibraryRow } from '../ui/LibraryRow';
import type { AcquisitionStatus, TrackResponse } from '../../../shared/api-client/types';

jest.mock('expo-image', () => ({ Image: () => null }));

function _track(acquisitionStatus: AcquisitionStatus, failureReason: string | null = null): TrackResponse {
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

describe('LibraryRow retry', () => {
  it('shows Retry button when onRetry is provided and status is failed', () => {
    const { getByTestId } = render(
      <LibraryRow track={_track('failed')} onPress={noop} onMore={noop} onRetry={noop} />,
    );
    expect(getByTestId('library-row-retry-t1')).toBeTruthy();
  });

  it('hides Retry button when onRetry is undefined', () => {
    const { queryByTestId } = render(
      <LibraryRow track={_track('failed')} onPress={noop} onMore={noop} />,
    );
    expect(queryByTestId('library-row-retry-t1')).toBeNull();
  });

  it('fires onRetry callback on press', () => {
    const onRetry = jest.fn();
    const { getByTestId } = render(
      <LibraryRow track={_track('failed')} onPress={noop} onMore={noop} onRetry={onRetry} />,
    );
    fireEvent.press(getByTestId('library-row-retry-t1'));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('shows ActivityIndicator when retrying is true', () => {
    const { getByTestId, queryByTestId } = render(
      <LibraryRow track={_track('failed')} onPress={noop} onMore={noop} onRetry={noop} retrying />,
    );
    expect(getByTestId('library-row-retrying-t1')).toBeTruthy();
    expect(queryByTestId('library-row-retry-t1')).toBeNull();
  });

  it('shows "Retrying…" text when retrying is true', () => {
    const { getByTestId } = render(
      <LibraryRow track={_track('failed')} onPress={noop} onMore={noop} onRetry={noop} retrying />,
    );
    const failedEl = getByTestId('library-row-failed-t1');
    expect(failedEl.props.children).toBe('Retrying…');
  });

  it('shows human-readable failure reason instead of raw server string', () => {
    const { getByTestId } = render(
      <LibraryRow track={_track('failed', 'no_match_found')} onPress={noop} onMore={noop} />,
    );
    const failedEl = getByTestId('library-row-failed-t1');
    expect(failedEl.props.children).toBe("Couldn't find this track");
  });
});
