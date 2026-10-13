import React from 'react';
import { act, render, waitFor } from '@testing-library/react-native';
import { asTrackId } from '@shared/apiClient/ids';
import { _resetAcquisitionTelemetryForTest } from '@shared/acquisition/state/acquisitionTelemetry';
import type { FailedAcquisition, TrackFields } from '@shared/apiClient/trackResponse';
import { trackResponseFactory } from '@testing/factories';
import { signInTestUser } from '@testing/signInTestUser';
import { LibraryRowFailure } from '@features/library/ui/LibraryRowFailure';

const { __http } = require('@testing/doubles/fetch.js');

const TELEMETRY_ROUTE = 'POST /v1/discovery/events';
const HTTP_ACCEPTED = 202;

type FailedTrack = TrackFields & FailedAcquisition;

function failedTrack(overrides: Partial<FailedTrack> = {}): FailedTrack {
  return trackResponseFactory.build({
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
  }) as FailedTrack;
}

interface SentEvent {
  type: string;
  payload: Record<string, unknown>;
}

function sentEvents(): SentEvent[] {
  return (__http.requests as { path: string; body: string }[])
    .filter((request) => request.path === '/v1/discovery/events')
    .map((request) => {
      const { type, payload } = JSON.parse(request.body) as SentEvent;
      delete payload['session_id'];
      return { type, payload };
    });
}

async function settleTelemetry(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

beforeEach(async () => {
  await signInTestUser();
  __http.reply(TELEMETRY_ROUTE, { status: HTTP_ACCEPTED });
  _resetAcquisitionTelemetryForTest();
});

describe('LibraryRowFailure — failure_shown telemetry', () => {
  it('records failure_shown with the displayed message once rendered', async () => {
    render(<LibraryRowFailure track={failedTrack()} retrying={false} onRetry={jest.fn()} />);

    await waitFor(() =>
      expect(sentEvents()).toEqual([
        {
          type: 'acquisition_ui',
          payload: { track_id: 't-1', action: 'failure_shown', message: 'No source found' },
        },
      ]),
    );
  });

  it('does not record while the row shows its retrying state', async () => {
    render(<LibraryRowFailure track={failedTrack()} retrying onRetry={jest.fn()} />);
    await settleTelemetry();

    expect(sentEvents()).toEqual([]);
  });

  it('does not repeat the same message on a re-render', async () => {
    const { rerender } = render(
      <LibraryRowFailure track={failedTrack()} retrying={false} onRetry={jest.fn()} />,
    );
    rerender(<LibraryRowFailure track={failedTrack()} retrying={false} onRetry={jest.fn()} />);

    await settleTelemetry();

    expect(sentEvents()).toHaveLength(1);
  });
});
