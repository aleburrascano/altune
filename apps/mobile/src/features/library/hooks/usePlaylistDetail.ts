import { useQuery } from '@tanstack/react-query';

import type { PlaylistId } from '@shared/api-client/ids';
import { getPlaylist } from '@shared/api-client/playlists';
import { playlistKeys } from '@shared/lib/query-keys';

// An empty id (missing route param) skips the fetch.
export function usePlaylistDetail(playlistId: PlaylistId) {
  return useQuery({
    queryKey: playlistKeys.detail(playlistId),
    queryFn: () => getPlaylist(playlistId),
    enabled: playlistId.length > 0,
    staleTime: Infinity,
  });
}
