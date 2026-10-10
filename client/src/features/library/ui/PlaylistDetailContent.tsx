import { useState, type ReactElement, type ComponentProps } from 'react';
import { Screen } from '@shared/ui';
import { usePlaylistRename } from '@features/library/hooks/usePlaylistRename';
import type {
  ContentProps,
  DetailActions,
  DetailProps,
} from '@features/library/ui/playlistDetailTypes';
import { PlaylistTopBar } from '@features/library/ui/PlaylistTopBar';
import { PlaylistTrackList as PlaylistTracks } from '@features/library/ui/PlaylistTrackList';
import { useAddTracksToPlaylist } from '@shared/playlists';
import { AddTracksToPlaylistModal } from '@features/library/ui/AddTracksToPlaylistModal';

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

type ModalProps = ComponentProps<typeof AddTracksToPlaylistModal>;

function useAddTracks(props: DetailProps): Pick<ModalProps, 'adding' | 'onAdd'> {
  const addMut = useAddTracksToPlaylist();
  const { playlistId, onCloseAdd } = props;
  const onAdd: ModalProps['onAdd'] = (trackIds) => {
    addMut.mutate({ playlistId, trackIds }, { onSuccess: onCloseAdd });
  };
  return { adding: addMut.isPending, onAdd };
}

function addModalProps(props: DetailProps): Omit<ModalProps, 'adding' | 'onAdd'> {
  return {
    visible: props.addVisible,
    playlistName: props.playlist.name,
    existingTrackIds: props.playlist.tracks.map((t) => t.id),
    onClose: props.onCloseAdd,
  };
}

function PlaylistAddTracks(props: DetailProps): ReactElement {
  const add = useAddTracks(props);
  return <AddTracksToPlaylistModal {...addModalProps(props)} {...add} />;
}
