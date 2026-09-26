import { usePinnedStore, type UnpinAllOutcome } from '@shared/offline/pinnedStore';
import type { DownloadStats } from '../downloadStatsModel';

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
