import TrackPlayer, { Capability } from 'react-native-track-player';

export const PLAYER_SETUP_TIMEOUT_MS = 15_000;

export class PlayerSetupTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback setup timed out after ${timeoutMs / 1000}s`);
    this.name = 'PlayerSetupTimeoutError';
  }
}

let setupPromise: Promise<void> | null = null;

export function ensurePlayerSetup(): Promise<void> {
  setupPromise ??= setupWithinBudget().catch((error: unknown) => {
    setupPromise = null;
    throw error;
  });
  return setupPromise;
}

function setupWithinBudget(): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pending = setup();
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
