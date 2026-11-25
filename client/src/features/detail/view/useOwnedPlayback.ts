import type { DiscoveryResult } from '@shared/catalog';
import type { RetryEntryPoint } from '@shared/acquisition';
import { useQueuePlayback } from '@shared/playback';

import { trackExtras } from '@features/detail/core/trackExtras';
import {
  playButtonState,
  splitOwned,
  toPlaybackQueue,
  type OwnedSplit,
  ownedFromTrackInfo,
  type OwnedTrack,
} from '@features/detail/core/ownedTrack';
import {
  ownedRetryTrackId,
  saveControlInteractive,
  saveControlState,
  toCreateTrackRequest,
} from '@features/detail/core/saveTrack';
import { resolveOwnedTrackAtActionTime } from '@features/detail/core/useOwnedTrack';
import { useRetryTrack } from '@features/detail/view/useRetryTrack';
import type { SaveTrack } from '@features/detail/view/useSaveTrack';

export type OwnedPlaybackContext = {
  title: string | null;
  image: string | null;
  enrich: (track: DiscoveryResult) => DiscoveryResult;
  retryEntryPoint: RetryEntryPoint;
};

export type OwnedPlayback = {
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  onQuickSave: (track: DiscoveryResult) => void;
};

function ownedFor(track: DiscoveryResult): OwnedTrack | null {
  return ownedFromTrackInfo(trackExtras(track.extras));
}

export function useOwnedPlayback(
  tracks: readonly DiscoveryResult[],
  context: OwnedPlaybackContext,
  save: SaveTrack,
): OwnedPlayback {
  const queue = useQueuePlayback();
  const retry = useRetryTrack(context.retryEntryPoint);

  const owned = splitOwned(tracks);

  const onPlayOwned = (): void => {
    const playable = toPlaybackQueue(owned.playable, context.title, context.image);
    if (playable.length === 0) return;
    queue.playFromList(playable, 0, { kind: 'library' });
  };

  const onQuickSave = (track: DiscoveryResult): void => {
    const enriched = context.enrich(track);
    const resolved = resolveOwnedTrackAtActionTime(ownedFor(track), {
      title: enriched.title,
      artist: enriched.subtitle,
    });
    if (!saveControlInteractive(saveControlState(resolved))) return;
    const retryId = ownedRetryTrackId(resolved);
    if (retryId !== null) {
      retry.mutate(retryId);
      return;
    }
    save.mutate(toCreateTrackRequest(enriched));
  };

  return {
    owned,
    playButton: playButtonState(owned),
    onPlayOwned,
    ownedFor,
    onQuickSave,
  };
}
