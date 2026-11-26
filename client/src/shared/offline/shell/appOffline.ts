import { createOffline } from '@shared/offline/shell/pinnedStore';

export const {
  usePinnedStore,
  resolvePinnedUri,
  repinIfPinned,
  providePort: provideOfflinePort,
  dispose: stopOffline,
} = createOffline({
  now: () => performance.now(),
  random: () => Math.random(),
});
