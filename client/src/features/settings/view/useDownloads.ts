import { useMemo } from 'react';

import { pinnedBytes, usePinnedStore, type PinnedEntry } from '@shared/offline';
import { downloadStats, type DownloadStats } from '@features/settings/core/downloadStatsModel';
import type { RemoveDownloads } from '@features/settings/core/removeDownloads';

function measureAfter(_entries: Record<string, PinnedEntry>): number {
  return pinnedBytes();
}

export function useDownloadStats(): DownloadStats {
  const entries = usePinnedStore((s) => s.entries);
  const bytes = useMemo(() => measureAfter(entries), [entries]);
  return downloadStats(entries, bytes);
}

export function useRemoveDownloads(stats: DownloadStats): RemoveDownloads {
  const unpinAll = usePinnedStore((s) => s.unpinAll);
  const lastUnpinAll = usePinnedStore((s) => s.lastUnpinAll);
  return lastUnpinAll === undefined ? { stats, unpinAll } : { stats, lastUnpinAll, unpinAll };
}
