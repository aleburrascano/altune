import { useState, type ReactElement } from 'react';

import { Screen } from '@shared/ui';

import { usePlaylistRename } from '@features/library/hooks/usePlaylistRename';
import type { ContentProps, DetailActions } from '@features/library/ui/playlistDetailTypes';
import { PlaylistAddTracks } from '@features/library/ui/PlaylistAddTracks';
import { PlaylistTopBar } from '@features/library/ui/PlaylistTopBar';
import { PlaylistTrackList as PlaylistTracks } from '@features/library/ui/PlaylistTrackList';

export type { ContentProps } from '@features/library/ui/playlistDetailTypes';

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
