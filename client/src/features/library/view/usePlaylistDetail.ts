import { useQuery } from '@tanstack/react-query';
import type { PlaylistId } from '@shared/catalog';
import { playlistKeys } from '@shared/query';
import { useLibraryPort } from '@features/library/view/LibraryPortProvider';
import { useEffect } from 'react';
import { failureLogFields } from '@features/library/domain/libraryFailure';

export function usePlaylistDetail(playlistId: PlaylistId) {
  const port = useLibraryPort();
  return useQuery({
    queryKey: playlistKeys.detail(playlistId),
    queryFn: () => port.getPlaylist(playlistId),
    enabled: playlistId.length > 0,
    staleTime: Infinity,
  });
}

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
