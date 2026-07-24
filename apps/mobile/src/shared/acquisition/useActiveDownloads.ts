import { useActiveDownloadItems, type DownloadEntry } from './downloadStore';

export function useActiveDownloads(): DownloadEntry[] {
  return useActiveDownloadItems();
}

export type { DownloadEntry as DownloadItem };
