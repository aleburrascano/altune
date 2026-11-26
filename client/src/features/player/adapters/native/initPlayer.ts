import TrackPlayer, { Capability } from 'react-native-track-player';

import { warnPlayback } from '@features/player/domain/playbackError';
import { withDeadline } from '@shared/timing';

const PLAYER_SETUP_TIMEOUT_MS = 15_000;

class PlayerSetupTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback setup timed out after ${timeoutMs / 1000}s`);
    this.name = 'PlayerSetupTimeoutError';
  }
}

export type SetupPlayer = Pick<typeof TrackPlayer, 'setupPlayer' | 'updateOptions'>;

export function createEnsurePlayerSetup(player: SetupPlayer): () => Promise<void> {
  let setupPromise: Promise<void> | null = null;
  return () => {
    setupPromise ??= setupWithinBudget(player).catch((error: unknown) => {
      setupPromise = null;
      throw error;
    });
    return setupPromise;
  };
}

export const ensurePlayerSetup = createEnsurePlayerSetup(TrackPlayer);

function setupWithinBudget(player: SetupPlayer): Promise<void> {
  return withDeadline(setup(player), PLAYER_SETUP_TIMEOUT_MS, {
    onExpire: () => new PlayerSetupTimeoutError(PLAYER_SETUP_TIMEOUT_MS),
    onLate: (err) => warnPlayback('player setup failed after its deadline', {}, err),
  });
}

async function setup(player: SetupPlayer): Promise<void> {
  await player.setupPlayer({ autoHandleInterruptions: true });
  await player.updateOptions({
    capabilities: [
      Capability.Play,
      Capability.Pause,
      Capability.SeekTo,
      Capability.SkipToNext,
      Capability.SkipToPrevious,
    ],
    compactCapabilities: [Capability.Play, Capability.Pause, Capability.SkipToNext],
  });
}
