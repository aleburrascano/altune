import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { RetryEntryPoint } from '@shared/acquisition/acquisitionTelemetry';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

import { trackExtras } from '@/features/detail/domain/trackExtras';
import {
  playButtonState,
  splitOwned,
  toPlaybackQueue,
  type OwnedSplit,
  ownedFromExtras,
  type OwnedTrack,
} from '@/features/detail/domain/ownedTrack';
import {
  ownedRetryTrackId,
  saveControlInteractive,
  saveControlState,
  toCreateTrackRequest,
} from '@/features/detail/domain/saveTrack';
import { resolveOwnedTrackAtActionTime } from '@/features/detail/hooks/useOwnedTrack';
import { useRetryTrack } from '@/features/detail/hooks/useRetryTrack';
import type { SaveTrack } from '@/features/detail/hooks/useSaveTrack';

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

  const ownedFor = (track: DiscoveryResult): OwnedTrack | null =>
    ownedFromExtras(trackExtras(track.extras));

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
