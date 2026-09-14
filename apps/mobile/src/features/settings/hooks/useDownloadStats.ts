import {
  formatBytes,
  pinnedByteTotal,
  usePinnedStore,
  type PinnedEntry,
} from '@shared/offline/pinnedStore';
import { countLabel } from '@shared/lib/format';

export type DownloadStats = {
  downloadCount: number;
  downloadSize: string;
  usageLabel: string;
  usageDetail: string | undefined;
};

export function downloadStats(entries: Record<string, PinnedEntry>, bytes: number): DownloadStats {
  const downloadCount = Object.values(entries).filter((e) => e.status === 'ready').length;
  const downloadSize = formatBytes(bytes);
  return {
    downloadCount,
    downloadSize,
    usageLabel:
      downloadCount === 0
        ? 'No downloads on this device'
        : `${downloadCount} ${countLabel(downloadCount, 'track')}`,
    usageDetail: downloadCount === 0 ? undefined : downloadSize,
  };
}

export function useDownloadStats(): DownloadStats {
  const entries = usePinnedStore((s) => s.entries);
  return downloadStats(entries, pinnedByteTotal());
}
