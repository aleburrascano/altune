import type { TrackId } from '@shared/api-client/ids';
import { trackNotFoundAlert } from '@features/library/domain/trackNotFoundAlert';

describe('trackNotFoundAlert', () => {
  it('builds the exact payload for the track', () => {
    expect(trackNotFoundAlert('track-1' as TrackId)).toEqual({
      surface: 'library.track_not_found',
      title: 'Track not found',
      message: 'This track is no longer in your library.',
      trackId: 'track-1',
    });
  });

  it('carries the id it was given', () => {
    expect(trackNotFoundAlert('track-2' as TrackId)).toEqual({
      surface: 'library.track_not_found',
      title: 'Track not found',
      message: 'This track is no longer in your library.',
      trackId: 'track-2',
    });
  });
});
