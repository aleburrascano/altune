import { useEffect } from 'react';

import type { PlaylistId } from '@shared/api-client/ids';

import { failureLogFields } from '../failureLogFields';

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
