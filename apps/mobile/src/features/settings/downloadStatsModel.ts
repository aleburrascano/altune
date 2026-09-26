import { formatBytes } from '@shared/offline/pinnedFiles';
import type { PinnedEntry } from '@shared/offline/pinnedIndex';
import { countLabel } from '@shared/lib/format';

export type DownloadUsage = 'none' | 'leftover' | 'tracks';

export type DownloadStats = {
  downloadCount: number;
  downloadBytes: number;
  downloadSize: string;
  usage: DownloadUsage;
  usageLabel: string;
  usageDetail: string | undefined;
};

export const LEFTOVER_FILES_LABEL = 'Leftover download files';

export function downloadUsage(count: number, bytes: number): DownloadUsage {
  if (count > 0) return 'tracks';
  return bytes > 0 ? 'leftover' : 'none';
}

export function tracksLabel(count: number): string {
  return `${count} ${countLabel(count, 'track')}`;
}

function usageLabelFor(usage: DownloadUsage, count: number): string {
  switch (usage) {
    case 'tracks':
      return tracksLabel(count);
    case 'leftover':
      return LEFTOVER_FILES_LABEL;
    case 'none':
      return 'No downloads on this device';
  }
}

function readyCount(entries: Record<string, PinnedEntry>): number {
  return Object.values(entries).filter((e) => e.status === 'ready').length;
}

function usageFields(usage: DownloadUsage, count: number, size: string): Pick<DownloadStats, 'usage' | 'usageLabel' | 'usageDetail'> {
  return { usage, usageLabel: usageLabelFor(usage, count), usageDetail: usage === 'none' ? undefined : size };
}

export function downloadStats(entries: Record<string, PinnedEntry>, bytes: number): DownloadStats {
  const downloadCount = readyCount(entries);
  const downloadSize = formatBytes(bytes);
  const usage = downloadUsage(downloadCount, bytes);
  return { downloadCount, downloadBytes: bytes, downloadSize, ...usageFields(usage, downloadCount, downloadSize) };
}
