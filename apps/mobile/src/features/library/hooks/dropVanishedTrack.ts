import { showFailureAlert } from '@shared/ui';
import type { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { forgetTrack } from '@shared/events/forgetTrack';

export function dropVanishedTrack(queryClient: QueryClient, trackId: TrackId): void {
  forgetTrack(queryClient, trackId);
  showFailureAlert({
    surface: 'library.track_not_found',
    title: 'Track not found',
    message: 'This track is no longer in your library.',
    trackId,
  });
}
