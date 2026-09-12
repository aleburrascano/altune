import type { DiscoveryResult } from '@shared/api-client/discovery';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

import { trackExtras } from '../extras-accessors';
import {
  playButtonState,
  splitOwned,
  toPlaybackQueue,
  type OwnedSplit,
} from '../owned-playback';
import { toCreateTrackRequest } from '../save-cache';
import { saveControlState, type SaveControlState } from '../save-control-state';
import { ownedFromExtras } from './useOwnedTrack';
import type { useSaveTrack } from './useSaveTrack';

export type OwnedPlaybackContext = {
  title: string | null;
  image: string | null;
  enrich: (track: DiscoveryResult) => DiscoveryResult;
};

export type OwnedPlayback = {
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
  saveStateFor: (track: DiscoveryResult) => SaveControlState;
  onQuickSave: (track: DiscoveryResult) => void;
};

export function useOwnedPlayback(
  tracks: readonly DiscoveryResult[],
  context: OwnedPlaybackContext,
  save: ReturnType<typeof useSaveTrack>,
): OwnedPlayback {
  const queue = useQueuePlayback();

  const owned = splitOwned(tracks);

  const onPlayOwned = (): void => {
    const playable = toPlaybackQueue(owned.playable, context.title, context.image);
    if (playable.length === 0) return;
    queue.playFromList(playable, 0, { kind: 'library' });
  };

  const saveStateFor = (track: DiscoveryResult): SaveControlState =>
    saveControlState(ownedFromExtras(trackExtras(track.extras)));

  const onQuickSave = (track: DiscoveryResult): void => {
    save.mutate(toCreateTrackRequest(context.enrich(track)));
  };

  return {
    owned,
    playButton: playButtonState(owned),
    onPlayOwned,
    saveStateFor,
    onQuickSave,
  };
}
