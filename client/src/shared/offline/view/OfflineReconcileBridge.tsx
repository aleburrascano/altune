import { useEffect, type ReactElement } from 'react';

import { offlineDownloadsSupported } from '@shared/offline/core/offlineSupport';

type OfflineReconcileBridgeProps = { reconcile: () => void };

export function OfflineReconcileBridge({
  reconcile,
}: OfflineReconcileBridgeProps): ReactElement | null {
  useEffect(() => {
    if (offlineDownloadsSupported) reconcile();
  }, [reconcile]);
  return null;
}
