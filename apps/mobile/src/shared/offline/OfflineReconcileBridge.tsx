import { useEffect, type ReactElement } from 'react';

import { usePinnedStore } from './pinnedStore';

export function OfflineReconcileBridge(): ReactElement | null {
  const reconcile = usePinnedStore((s) => s.reconcile);
  useEffect(() => {
    reconcile();
  }, [reconcile]);
  return null;
}
