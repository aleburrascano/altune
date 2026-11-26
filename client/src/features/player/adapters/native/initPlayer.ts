import TrackPlayer, { Capability } from 'react-native-track-player';

import { warnPlayback } from '@features/player/domain/playbackError';

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
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pending = setup(player);
  let hasTimedOut = false;
  pending.catch((err: unknown) => {
    if (hasTimedOut) warnPlayback('player setup failed after its deadline', {}, err);
  });
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      hasTimedOut = true;
      reject(new PlayerSetupTimeoutError(PLAYER_SETUP_TIMEOUT_MS));
    }, PLAYER_SETUP_TIMEOUT_MS);
  });
  return Promise.race([pending, deadline]).finally(() => clearTimeout(timer));
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
