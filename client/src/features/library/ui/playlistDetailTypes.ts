import type { useRouter } from 'expo-router';

import type { PlaylistId, PlaylistDetailResponse } from '@shared/apiClient';
import type { usePlaylistRename } from '@features/library/hooks/usePlaylistRename';

type Router = ReturnType<typeof useRouter>;

export type ContentProps = {
  playlistId: PlaylistId;
  playlist: PlaylistDetailResponse;
  refreshing: boolean;
  onRefresh: () => void;
  router: Router;
};

export type DetailActions = {
  rename: ReturnType<typeof usePlaylistRename>;
  onAddTracks: () => void;
  addVisible: boolean;
  onCloseAdd: () => void;
};

export type DetailProps = ContentProps & DetailActions;
