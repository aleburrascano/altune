import React from 'react';
import { render } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import { enqueueCritical } from '@shared/telemetry/outbox';
import { _resetAcquisitionTelemetryForTest } from '@shared/acquisition/acquisitionTelemetry';
import type { FailedAcquisition, TrackFields } from '@shared/api-client/types';

import { LibraryRowFailure } from '../LibraryRowFailure';

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const enqueueCriticalMock = enqueueCritical as jest.MockedFunction<typeof enqueueCritical>;

type FailedTrack = TrackFields & FailedAcquisition;

function failedTrack(overrides: Partial<FailedTrack> = {}): FailedTrack {
  return {
    id: asTrackId('t-1'),
    title: 'Idioteque',
    artist: 'Radiohead',
    album: null,
    duration_seconds: 300,
    added_at: '2026-01-01T00:00:00Z',
    artwork_url: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    acquisition_status: 'failed',
    failure_reason: 'no_source',
    failure_message: 'No source found',
    ...overrides,
  } as FailedTrack;
}

function lastPayload(): Record<string, unknown> {
  const call = enqueueCriticalMock.mock.calls.at(-1);
  return (call?.[0]?.payload ?? {}) as Record<string, unknown>;
}

beforeEach(() => {
  enqueueCriticalMock.mockReset().mockResolvedValue(undefined);
  _resetAcquisitionTelemetryForTest();
});

describe('LibraryRowFailure — failure_shown telemetry', () => {
  it('records failure_shown with the displayed message once rendered', () => {
    render(<LibraryRowFailure track={failedTrack()} retrying={false} onRetry={jest.fn()} />);

    expect(enqueueCriticalMock).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'acquisition_ui' }),
    );
    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'failure_shown',
      message: 'No source found',
    });
  });

  it('does not record while the row shows its retrying state', () => {
    render(<LibraryRowFailure track={failedTrack()} retrying onRetry={jest.fn()} />);

    expect(enqueueCriticalMock).not.toHaveBeenCalled();
  });

  it('does not repeat the same message on a re-render', () => {
    const { rerender } = render(
      <LibraryRowFailure track={failedTrack()} retrying={false} onRetry={jest.fn()} />,
    );
    rerender(<LibraryRowFailure track={failedTrack()} retrying={false} onRetry={jest.fn()} />);

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(1);
  });
});
