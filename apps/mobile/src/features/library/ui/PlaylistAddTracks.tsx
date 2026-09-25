import type { ComponentProps, ReactElement } from 'react';

import { useAddTracksToPlaylist } from '@shared/playlists';

import { AddTracksToPlaylistModal } from './AddTracksToPlaylistModal';
import type { DetailProps } from './playlistDetailTypes';

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

export function PlaylistAddTracks(props: DetailProps): ReactElement {
  const add = useAddTracks(props);
  return <AddTracksToPlaylistModal {...addModalProps(props)} {...add} />;
}
