import type { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { removeTrackStatus } from '@shared/acquisition/trackStatusStore';

import { removeTrackFromCaches } from './trackCachePatch';

export function forgetTrack(queryClient: QueryClient, trackId: TrackId): void {
  removeTrackFromCaches(queryClient, trackId);
  removeTrackStatus(trackId);
}
