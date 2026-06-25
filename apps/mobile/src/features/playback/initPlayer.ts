import TrackPlayer, { Capability } from 'react-native-track-player';

// AIDEV-NOTE: Player setup is promise-based (not a bool flag) so concurrent
// callers — the provider's mount effect and queue-resume's cold-start priming —
// await the SAME setup instead of racing two setupPlayer() calls. ensurePlayerSetup
// is idempotent: the first call runs setup, every later call awaits its result.
let setupPromise: Promise<void> | null = null;

export function ensurePlayerSetup(): Promise<void> {
  if (!setupPromise) {
    setupPromise = setup();
  }
  return setupPromise;
}

async function setup(): Promise<void> {
  await TrackPlayer.setupPlayer({});
  await TrackPlayer.updateOptions({
    capabilities: [
      Capability.Play,
      Capability.Pause,
      Capability.SeekTo,
      Capability.SkipToNext,
      Capability.SkipToPrevious,
    ],
    compactCapabilities: [
      Capability.Play,
      Capability.Pause,
      Capability.SkipToNext,
    ],
  });
}
