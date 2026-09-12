import type { ReactElement } from 'react';

import type { TrackResponse } from '@shared/api-client/types';
import { countLabel } from '@shared/lib/format';
import { AddToPlaylistSheet } from '@shared/playlists';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';

import type { TrackSelectionController } from '../hooks/useTrackSelection';
import { SelectionBar } from './SelectionBar';

type TrackSelectionOverlayProps = {
  controller: TrackSelectionController;
  tracks: TrackResponse[];
  barVisible: boolean;
};

/**
 * Presenter for the shared track-selection surface: the SelectionBar (with
 * select-all), the bulk AddToPlaylistSheet, and the track-action ContextMenu.
 * All state lives in the `controller` (useTrackSelection); this only renders it.
 */
export function TrackSelectionOverlay({
  controller,
  tracks,
  barVisible,
}: TrackSelectionOverlayProps): ReactElement {
  const { selection, trackAction } = controller;

  return (
    <>
      {barVisible ? (
        <SelectionBar
          count={selection.count}
          allSelected={controller.allSelected(tracks)}
          onSelectAll={() => controller.toggleSelectAll(tracks)}
          onCancel={selection.clear}
          actions={controller.selectionActionsFor(tracks)}
        />
      ) : null}

      <AddToPlaylistSheet
        visible={controller.bulkSheetVisible}
        label={`${selection.count} ${countLabel(selection.count, 'track')}`}
        resolveTrackIds={() => Promise.resolve(selection.ids)}
        onClose={controller.closeBulkSheet}
      />

      <ContextMenu
        visible={trackAction != null}
        anchor={trackAction?.anchor}
        items={trackAction != null ? controller.trackMenuItems(trackAction.track) : []}
        onClose={controller.closeTrackMenu}
      />
    </>
  );
}
