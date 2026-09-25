import { useState, type ReactElement } from 'react';

import { Screen } from '@shared/ui';

import { usePlaylistRename } from '../hooks/usePlaylistRename';
import type { ContentProps, DetailActions } from './playlistDetailTypes';
import { PlaylistAddTracks } from './PlaylistAddTracks';
import { PlaylistTopBar } from './PlaylistTopBar';
import { PlaylistTrackList as PlaylistTracks } from './PlaylistTrackList';

export type { ContentProps } from './playlistDetailTypes';

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
