import { useMemo } from 'react';

import { pinnedByteTotal, usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';
import { downloadStats, type DownloadStats } from '../downloadStatsModel';

function measureAfter(_entries: Record<string, PinnedEntry>): number {
  return pinnedByteTotal();
}

export function useDownloadStats(): DownloadStats {
  const entries = usePinnedStore((s) => s.entries);
  const bytes = useMemo(() => measureAfter(entries), [entries]);
  return downloadStats(entries, bytes);
}
