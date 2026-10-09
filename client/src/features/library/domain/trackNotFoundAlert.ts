import type { TrackId } from '@shared/api-client/ids';
import type { FailureAlert } from '@shared/ui';

export function trackNotFoundAlert(trackId: TrackId): FailureAlert {
  return {
    surface: 'library.track_not_found',
    title: 'Track not found',
    message: 'This track is no longer in your library.',
    trackId,
  };
}
