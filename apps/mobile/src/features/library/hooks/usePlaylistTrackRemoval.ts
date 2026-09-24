import type { PlaylistId } from '@shared/api-client/ids';
import { countLabel } from '@shared/lib/format';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { useRemoveTracksFromPlaylist } from '@shared/playlists';
import { confirmDestructive } from '@shared/ui/confirmDestructive';

import type { useLibraryNavigation } from './useLibraryNavigation';
import { useTrackSelection, type TrackSelectionController } from './useTrackSelection';

export function usePlaylistTrackRemoval(
  playlistId: PlaylistId,
  playlistName: string,
  queue: ReturnType<typeof useQueuePlayback>,
  navigateToTrack: ReturnType<typeof useLibraryNavigation>['navigateToTrack'],
): TrackSelectionController {
  const removeMut = useRemoveTracksFromPlaylist(playlistId);

  return useTrackSelection({
    queue,
    onViewDetails: navigateToTrack,
    trackDanger: (track) => ({
      label: 'Remove from Playlist',
      onPress: () => removeMut.mutate([track.id]),
    }),
    selectionDanger: {
      label: 'Remove',
      onRemove: (ids, clear) =>
        confirmDestructive({
          title: 'Remove from Playlist',
          message: `Remove ${ids.length} ${countLabel(ids.length, 'track')} from ${playlistName}?`,
          confirmLabel: 'Remove',
          onConfirm: () => {
            removeMut.mutate(ids);
            clear();
          },
        }),
    },
  });
}
