import type { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';

import type { PlaylistId } from '@shared/api-client/ids';
import { Screen } from '@shared/ui';
import type { PlaylistDetailResponse } from '@shared/api-client/types';

import { usePlaylistRename } from '../hooks/usePlaylistRename';
import { PlaylistAddTracks } from './PlaylistAddTracks';
import { PlaylistTopBar } from './PlaylistTopBar';
import { PlaylistTrackList as PlaylistTracks } from './PlaylistTrackList';

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

function usePlaylistDetailActions(props: ContentProps): DetailActions {
  const [addVisible, setAddVisible] = useState(false);
  return {
    rename: usePlaylistRename(props.playlistId, props.playlist.name),
    onAddTracks: () => setAddVisible(true),
    onCloseAdd: () => setAddVisible(false),
    addVisible,
  };
}

export function PlaylistDetailContent(props: ContentProps): ReactElement {
  const actions = usePlaylistDetailActions(props);
  return (
    <Screen padded={false}>
      <PlaylistTopBar {...props} {...actions} />
      <PlaylistTracks {...props} {...actions} />
      <PlaylistAddTracks {...props} {...actions} />
    </Screen>
  );
}
