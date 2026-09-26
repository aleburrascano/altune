import type { DiscoveryResult } from '@shared/api-client/discovery';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

import { trackExtras } from '../extras-accessors';
import {
  playButtonState,
  splitOwned,
  toPlaybackQueue,
  type OwnedSplit,
} from '../owned-playback';
import { ownedRetryTrackId } from '../save-control-state';
import { toCreateTrackRequest } from '../save-cache';
import { ownedFromExtras, resolveOwnedTrackAtActionTime, type OwnedTrack } from './useOwnedTrack';
import { useRetryTrack } from './useRetryTrack';
import type { SaveTrack } from './useSaveTrack';

export type OwnedPlaybackContext = {
  title: string | null;
  image: string | null;
  enrich: (track: DiscoveryResult) => DiscoveryResult;
};

export type OwnedPlayback = {
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
  // The row's own owning extras, resolved through the shared owned-track rule by
  // TrackSaveControl. Not a precomputed status: the stamped trackId must reach
  // the rule so the save control and the detail rows never disagree (#748).
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  onQuickSave: (track: DiscoveryResult) => void;
};

export function useOwnedPlayback(
  tracks: readonly DiscoveryResult[],
  context: OwnedPlaybackContext,
  save: SaveTrack,
): OwnedPlayback {
  const queue = useQueuePlayback();
  const retry = useRetryTrack();

  const owned = splitOwned(tracks);

  const onPlayOwned = (): void => {
    const playable = toPlaybackQueue(owned.playable, context.title, context.image);
    if (playable.length === 0) return;
    queue.playFromList(playable, 0, { kind: 'library' });
  };

  const ownedFor = (track: DiscoveryResult): OwnedTrack | null =>
    ownedFromExtras(trackExtras(track.extras));

  const onQuickSave = (track: DiscoveryResult): void => {
    const resolved = resolveOwnedTrackAtActionTime(ownedFor(track), {
      title: track.title,
      artist: track.subtitle,
    });
    const retryId = ownedRetryTrackId(resolved);
    if (retryId !== null) {
      retry.mutate(retryId);
      return;
    }
    save.mutate(toCreateTrackRequest(context.enrich(track)));
  };

  return {
    owned,
    playButton: playButtonState(owned),
    onPlayOwned,
    ownedFor,
    onQuickSave,
  };
}
