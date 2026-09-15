import TrackPlayer, { Capability } from 'react-native-track-player';

let setupPromise: Promise<void> | null = null;

export function ensurePlayerSetup(): Promise<void> {
  if (!setupPromise) {
    // A failed attempt must not stay cached, or every later call replays the stale rejection.
    setupPromise = setup().catch((error: unknown) => {
      setupPromise = null;
      throw error;
    });
  }
  return setupPromise;
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
