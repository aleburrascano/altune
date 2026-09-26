import { useMemo } from 'react';

import { pinnedByteTotal, usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';
import { downloadStats as downloadStatsModel } from '../downloadStatsModel';

export type DownloadStats = {
  downloadCount: number;
  downloadBytes: number;
  downloadSize: string;
  usageLabel: string;
  usageDetail: string | undefined;
};

export function downloadStats(entries: Record<string, PinnedEntry>, bytes: number): DownloadStats {
  const { downloadCount, downloadBytes, downloadSize, usageLabel, usageDetail } = downloadStatsModel(
    entries,
    bytes,
  );
  return { downloadCount, downloadBytes, downloadSize, usageLabel, usageDetail };
}

function measureAfter(_entries: Record<string, PinnedEntry>): number {
  return pinnedByteTotal();
}

export function useDownloadStats(): DownloadStats {
  const entries = usePinnedStore((s) => s.entries);
  const bytes = useMemo(() => measureAfter(entries), [entries]);
  return downloadStats(entries, bytes);
}
