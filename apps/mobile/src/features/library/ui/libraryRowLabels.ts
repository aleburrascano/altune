import type { DownloadPhase } from '@shared/acquisition/downloadStore';
import { phaseLabel } from '@shared/acquisition/stagePhase';
import type { PinnedStatus } from '@shared/offline/pinnedStore';

import { pinnedStatusDisplay } from '../pinnedStatusDisplay';

import type { AcquisitionStatus, TrackResponse } from '@shared/api-client/types';

export function albumSuffix(album: string | null): string {
  return album != null ? ` · ${album}` : '';
}

export function libraryRowAccessibilityLabel({
  track,
  retrying,
  canRetry,
  pinned,
}: {
  track: TrackResponse;
  retrying: boolean;
  canRetry: boolean;
  pinned: PinnedStatus | undefined;
}): string {
  const pendingLabel = track.acquisition_status === 'pending' ? ', pending' : '';
  const failedLabel = track.acquisition_status === 'failed' ? ', failed' : '';
  const retryLabel = retrying ? ', retrying' : canRetry ? ', retry available' : '';
  const offlineLabel = pinnedStatusDisplay(pinned).a11ySuffix;
  return `${track.title} by ${track.artist}${albumSuffix(track.album)}${pendingLabel}${failedLabel}${retryLabel}${offlineLabel}`;
}

/** The live download phase wins over the track's own status while one is running. */
export function acquisitionProgressLabel(
  phase: DownloadPhase | undefined,
  status: AcquisitionStatus,
): string | null {
  if (phase != null && phase !== 'failed') return phaseLabel(phase);
  if (status === 'pending') return 'Pending';
  return null;
}
