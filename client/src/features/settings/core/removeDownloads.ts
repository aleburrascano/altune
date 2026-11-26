import type { UnpinAllOutcome } from '@shared/offline';
import type { DownloadStats } from '@features/settings/core/downloadStatsModel';

export type RemoveDownloads = {
  stats: DownloadStats;
  lastUnpinAll?: UnpinAllOutcome;
  unpinAll: () => void;
};
