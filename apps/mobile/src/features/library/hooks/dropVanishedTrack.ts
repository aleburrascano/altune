import { Alert } from 'react-native';
import type { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { removeTrackStatus } from '@shared/acquisition/trackStatusStore';
import { removeTrackFromCaches } from '@shared/events/trackCachePatch';

/**
 * A track action answered "not found": the track was deleted elsewhere, so no retry
 * can succeed. Drop the stale row instead of leaving it offering the same action.
 */
export function dropVanishedTrack(queryClient: QueryClient, trackId: TrackId): void {
  removeTrackFromCaches(queryClient, trackId);
  removeTrackStatus(trackId);
  Alert.alert('Track not found', 'This track is no longer in your library.');
}
