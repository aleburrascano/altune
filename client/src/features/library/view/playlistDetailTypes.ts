import type { PlaylistId, PlaylistDetailResponse } from '@shared/catalog';
import type { Navigator } from '@shared/navigation';
import type { usePlaylistRename } from '@features/library/view/usePlaylistRename';

export type ContentProps = {
  playlistId: PlaylistId;
  playlist: PlaylistDetailResponse;
  refreshing: boolean;
  onRefresh: () => void;
  router: Navigator;
};

export type DetailActions = {
  rename: ReturnType<typeof usePlaylistRename>;
  onAddTracks: () => void;
  addVisible: boolean;
  onCloseAdd: () => void;
};

export type DetailProps = ContentProps & DetailActions;
