import { useEffect, type ReactElement } from 'react';

import { offlineDownloadsSupported } from '@shared/offline/domain/offlineSupport';
import { usePinnedStore } from '@shared/offline/state/pinnedStore';

export function OfflineReconcileBridge(): ReactElement | null {
  const reconcile = usePinnedStore((s) => s.reconcile);
  useEffect(() => {
    if (offlineDownloadsSupported) reconcile();
  }, [reconcile]);
  return null;
}
