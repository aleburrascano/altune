import { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { recordFailureShown } from '@shared/telemetry/userTelemetry';
import { showAlert } from '@shared/ui/dialog/dialog';

import { dropVanishedTrack } from '../hooks/dropVanishedTrack';

jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));
jest.mock('@shared/telemetry/userTelemetry', () => ({ recordFailureShown: jest.fn() }));

describe('dropVanishedTrack', () => {
  beforeEach(() => {
    jest.mocked(showAlert).mockClear();
    jest.mocked(recordFailureShown).mockClear();
  });

  it('tells the user through the dialog port that the track is gone', () => {
    dropVanishedTrack(new QueryClient(), 'track-1' as TrackId);

    expect(jest.mocked(showAlert).mock.calls).toEqual([
      ['Track not found', 'This track is no longer in your library.'],
    ]);
  });

  it('records failure_shown with the track id', () => {
    jest.mocked(recordFailureShown).mockClear();
    dropVanishedTrack(new QueryClient(), 'track-1' as TrackId);

    expect(jest.mocked(recordFailureShown).mock.calls).toEqual([
      [
        {
          surface: 'library.track_not_found',
          message: 'This track is no longer in your library.',
          track_id: 'track-1',
        },
      ],
    ]);
  });
});
