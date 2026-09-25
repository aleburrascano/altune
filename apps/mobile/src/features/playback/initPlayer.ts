import TrackPlayer, { Capability } from 'react-native-track-player';

// Budget for the one-shot native setup, the same order as a queue op's (see
// nativeQueueLock): a cold start still has to bind the playback service, which is
// slower than a bridge call, yet a bind that never comes back must not leave every
// later load, skip and reorder awaiting it for the rest of the session.
export const PLAYER_SETUP_TIMEOUT_MS = 15_000;

/**
 * Rejected to the caller whose setup outlived its budget. The native call cannot be
 * cancelled and may still settle later; its late result is discarded and the next
 * `ensurePlayerSetup` starts a fresh attempt.
 */
export class PlayerSetupTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback setup timed out after ${timeoutMs / 1000}s`);
    this.name = 'PlayerSetupTimeoutError';
  }
}

let setupPromise: Promise<void> | null = null;

export function ensurePlayerSetup(): Promise<void> {
  // A failed or timed-out attempt must not stay cached, or every later call replays the
  // stale rejection — or waits on a native call that already stopped coming back.
  setupPromise ??= setupWithinBudget().catch((error: unknown) => {
    setupPromise = null;
    throw error;
  });
  return setupPromise;
}

function setupWithinBudget(): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pending = setup();
  // A hung setup that rejects later must not surface as an unhandled rejection.
  pending.catch(() => undefined);
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(
      () => reject(new PlayerSetupTimeoutError(PLAYER_SETUP_TIMEOUT_MS)),
      PLAYER_SETUP_TIMEOUT_MS,
    );
  });
  return Promise.race([pending, deadline]).finally(() => clearTimeout(timer));
}

async function setup(): Promise<void> {
  await TrackPlayer.setupPlayer({ autoHandleInterruptions: true });
  await TrackPlayer.updateOptions({
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
