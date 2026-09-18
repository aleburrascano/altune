import { useEffect } from 'react';

import type { PlaylistId } from '@shared/api-client/ids';

import { failureLogFields } from '../failureLogFields';

/**
 * The detail screen answers a failed load with a static state either way, so without
 * this line "my playlist won't open" reaches triage with no id, no status and no
 * failure class (#1705). Redacted via `failureLogFields`: the caught error stays out.
 */
export function useLoggedPlaylistDetailFailure(playlistId: PlaylistId, error: Error | null): void {
  useEffect(() => {
    if (error === null) {
      return;
    }
    console.warn('[library] playlist detail query failed', {
      playlistId,
      ...failureLogFields(error),
    });
  }, [playlistId, error]);
}
