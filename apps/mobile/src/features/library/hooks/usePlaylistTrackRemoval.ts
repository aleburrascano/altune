import type { PlaylistId, TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { countLabel } from '@shared/lib/format';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { useRemoveTracksFromPlaylist } from '@shared/playlists';
import { confirmDestructive } from '@shared/ui/confirmDestructive';

import type { useLibraryNavigation } from './useLibraryNavigation';
import {
  useTrackSelection,
  type TrackSelectionController,
  type TrackSelectionOptions,
} from './useTrackSelection';

type PlaylistTrackRemovalArgs = {
  playlistId: PlaylistId;
  playlist: { name: string };
  queue: ReturnType<typeof useQueuePlayback>;
  navigateToTrack: ReturnType<typeof useLibraryNavigation>['navigateToTrack'];
};

type RemoveMutation = ReturnType<typeof useRemoveTracksFromPlaylist>;

function removalMessage(count: number, playlistName: string): string {
  return `Remove ${count} ${countLabel(count, 'track')} from ${playlistName}?`;
}

function confirmRemoval(message: string, onConfirm: () => void): void {
  confirmDestructive({ title: 'Remove from Playlist', message, confirmLabel: 'Remove', onConfirm });
}

function trackDanger(removeMut: RemoveMutation): TrackSelectionOptions['trackDanger'] {
  return (track: TrackResponse) => ({
    label: 'Remove from Playlist',
    onPress: () => removeMut.mutate([track.id]),
  });
}

function removeSelected(removeMut: RemoveMutation, ids: TrackId[], clear: () => void): () => void {
  return () => {
    removeMut.mutate(ids);
    clear();
  };
}

function selectionDanger(
  playlistName: string,
  removeMut: RemoveMutation,
): TrackSelectionOptions['selectionDanger'] {
  return {
    label: 'Remove',
    onRemove: (ids, clear) =>
      confirmRemoval(
        removalMessage(ids.length, playlistName),
        removeSelected(removeMut, ids, clear),
      ),
  };
}

export function usePlaylistTrackRemoval(args: PlaylistTrackRemovalArgs): TrackSelectionController {
  const removeMut = useRemoveTracksFromPlaylist(args.playlistId);
  return useTrackSelection({
    queue: args.queue,
    onViewDetails: args.navigateToTrack,
    trackDanger: trackDanger(removeMut),
    selectionDanger: selectionDanger(args.playlist.name, removeMut),
  });
}
