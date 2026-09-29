import { showAlert } from '@shared/ui/dialog/dialog';
import type { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { forgetTrack } from '@shared/events/forgetTrack';

export function dropVanishedTrack(queryClient: QueryClient, trackId: TrackId): void {
  forgetTrack(queryClient, trackId);
  showAlert('Track not found', 'This track is no longer in your library.');
}
