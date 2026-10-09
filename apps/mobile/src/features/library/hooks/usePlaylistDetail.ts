import { useQuery } from '@tanstack/react-query';

import type { PlaylistId } from '@shared/api-client/ids';
import { playlistKeys } from '@shared/query/queryKeys';

import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

export function usePlaylistDetail(playlistId: PlaylistId) {
  const port = useLibraryPort();
  return useQuery({
    queryKey: playlistKeys.detail(playlistId),
    queryFn: () => port.getPlaylist(playlistId),
    enabled: playlistId.length > 0,
    staleTime: Infinity,
  });
}
