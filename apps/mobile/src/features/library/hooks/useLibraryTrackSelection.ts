import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { countLabel } from '@shared/ui/displayFormat';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { confirmDestructive } from '@shared/ui/confirmDestructive';

import { useDeleteTrack } from '@features/library/hooks/useDeleteTrack';
import { useDeleteTracks } from '@features/library/hooks/useDeleteTracks';
import type { useLibraryNavigation } from '@features/library/hooks/useLibraryNavigation';
import type { PlaylistActionsState } from '@features/library/hooks/usePlaylistActions';
import {
  useTrackSelection,
  type TrackSelectionController,
} from '@features/library/hooks/useTrackSelection';

type Deps = {
  queue: ReturnType<typeof useQueuePlayback>;
  navigation: ReturnType<typeof useLibraryNavigation>;
  pl: PlaylistActionsState;
};

const REMOVE_TITLE = 'Remove from Library';

function confirmRemove(message: string, onConfirm: () => void): void {
  confirmDestructive({ title: REMOVE_TITLE, message, confirmLabel: 'Remove', onConfirm });
}

function removeTrackDanger(track: TrackResponse, remove: (id: TrackId) => void) {
  const message = `Remove "${track.title}" from your library?`;
  return { label: REMOVE_TITLE, onPress: () => confirmRemove(message, () => remove(track.id)) };
}

function removeManyDanger(removeMany: (ids: TrackId[]) => void) {
  const onRemove = (ids: TrackId[], clear: () => void): void => {
    const message = `Remove ${ids.length} ${countLabel(ids.length, 'track')} from your library?`;
    confirmRemove(message, () => {
      removeMany(ids);
      clear();
    });
  };
  return { label: 'Remove', onRemove };
}

function useRemovalDanger() {
  const deleteOne = useDeleteTrack();
  const deleteMany = useDeleteTracks();
  return {
    trackDanger: (track: TrackResponse) => removeTrackDanger(track, deleteOne.mutate),
    selectionDanger: removeManyDanger(deleteMany.mutate),
  };
}

export function useLibraryTrackSelection(deps: Deps): TrackSelectionController {
  const { queue, navigation, pl } = deps;
  return useTrackSelection({
    queue,
    onViewDetails: navigation.navigateToTrack,
    onAddTrackToPlaylist: (track) => pl.setAddToPlaylistTrack(track),
    ...useRemovalDanger(),
  });
}
