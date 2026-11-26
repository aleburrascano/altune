import { createOffline } from '@shared/offline/shell/pinnedStore';

export const {
  usePinnedStore,
  resolvePinnedUri,
  repinIfPinned,
  providePort: provideOfflinePort,
} = createOffline({
  now: () => performance.now(),
  random: () => Math.random(),
});
