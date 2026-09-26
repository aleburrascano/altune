import { useEffect, type ReactElement } from 'react';

import { offlineDownloadsSupported } from './offlineSupport';
import { usePinnedStore } from './pinnedStore';

export function OfflineReconcileBridge(): ReactElement | null {
  const reconcile = usePinnedStore((s) => s.reconcile);
  useEffect(() => {
    if (offlineDownloadsSupported) reconcile();
  }, [reconcile]);
  return null;
}
