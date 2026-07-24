/**
 * Reconciles the pinned index against what is actually on disk, once per launch.
 *
 * The index and the files can disagree: a restore-from-backup brings the JSON
 * without the audio, an OS cleanup or a crash mid-download leaves the reverse.
 * Trusting the index alone would show "downloaded" on a track that then fails to
 * play offline — the one thing this feature must never do. Reconciling also
 * re-queues anything a kill interrupted.
 */
import { useEffect, type ReactElement } from 'react';

import { usePinnedStore } from './pinnedStore';

export function OfflineReconcileBridge(): ReactElement | null {
  const reconcile = usePinnedStore((s) => s.reconcile);
  useEffect(() => {
    reconcile();
  }, [reconcile]);
  return null;
}
