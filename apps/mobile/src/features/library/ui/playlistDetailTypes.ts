import type { useRouter } from 'expo-router';

import type { PlaylistId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/types';

import type { usePlaylistRename } from '../hooks/usePlaylistRename';

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
