import { useMemo } from 'react';

import {
  pinnedByteTotal,
  usePinnedStore,
  type PinnedEntry,
  type UnpinAllOutcome,
} from '@shared/offline/pinnedStore';
import { downloadStats, type DownloadStats } from '@features/settings/domain/downloadStatsModel';

function measureAfter(_entries: Record<string, PinnedEntry>): number {
  return pinnedByteTotal();
}

export function useDownloadStats(): DownloadStats {
  const entries = usePinnedStore((s) => s.entries);
  const bytes = useMemo(() => measureAfter(entries), [entries]);
  return downloadStats(entries, bytes);
}

export type RemoveDownloads = {
  stats: DownloadStats;
  lastUnpinAll?: UnpinAllOutcome;
  unpinAll: () => void;
};

export function useRemoveDownloads(stats: DownloadStats): RemoveDownloads {
  const unpinAll = usePinnedStore((s) => s.unpinAll);
  const lastUnpinAll = usePinnedStore((s) => s.lastUnpinAll);
  return lastUnpinAll === undefined ? { stats, unpinAll } : { stats, lastUnpinAll, unpinAll };
}
