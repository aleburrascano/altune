import { useMemo } from 'react';

import {
  formatBytes,
  pinnedByteTotal,
  usePinnedStore,
  type PinnedEntry,
} from '@shared/offline/pinnedStore';
import { countLabel } from '@shared/lib/format';

export type DownloadStats = {
  downloadCount: number;
  // Bytes actually on disk; can be non-zero with no ready track (leftover files).
  downloadBytes: number;
  downloadSize: string;
  usageLabel: string;
  usageDetail: string | undefined;
};

// Leftover files from a failed delete keep the retry path open, so bytes on disk
// count as downloads even when no track is ready.
export function hasNoDownloads(downloadCount: number, bytes: number): boolean {
  return downloadCount === 0 && bytes === 0;
}

function usageLabel(downloadCount: number, bytes: number): string {
  if (downloadCount > 0) return `${downloadCount} ${countLabel(downloadCount, 'track')}`;
  return bytes > 0 ? 'Leftover download files' : 'No downloads on this device';
}

export function downloadStats(entries: Record<string, PinnedEntry>, bytes: number): DownloadStats {
  const downloadCount = Object.values(entries).filter((e) => e.status === 'ready').length;
  const downloadSize = formatBytes(bytes);
  return {
    downloadCount,
    downloadBytes: bytes,
    downloadSize,
    usageLabel: usageLabel(downloadCount, bytes),
    usageDetail: hasNoDownloads(downloadCount, bytes) ? undefined : downloadSize,
  };
}

function measureAfter(_entries: Record<string, PinnedEntry>): number {
  return pinnedByteTotal();
}

export function useDownloadStats(): DownloadStats {
  const entries = usePinnedStore((s) => s.entries);
  const bytes = useMemo(() => measureAfter(entries), [entries]);
  return downloadStats(entries, bytes);
}
