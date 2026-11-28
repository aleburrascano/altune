import { useQuery } from '@tanstack/react-query';
import type { PlaylistId } from '@shared/catalog';
import { playlistKeys, STALE } from '@shared/query';
import { useLibraryPort } from '@features/library/view/LibraryProvider';
import { useEffect } from 'react';
import { failureLogFields } from '@features/library/core/errors';

export function usePlaylistDetail(playlistId: PlaylistId) {
  const port = useLibraryPort();
  return useQuery({
    queryKey: playlistKeys.detail(playlistId),
    queryFn: ({ signal }) => port.getPlaylist(playlistId, signal),
    enabled: playlistId.length > 0,
    staleTime: STALE.untilEvent,
  });
}

export function useLoggedPlaylistDetailFailure(playlistId: PlaylistId, error: Error | null): void {
  const { warn } = useLibraryPort();
  useEffect(() => {
    if (error === null) {
      return;
    }
    warn('[library] playlist detail query failed', {
      playlistId,
      ...failureLogFields(error),
    });
  }, [warn, playlistId, error]);
}
