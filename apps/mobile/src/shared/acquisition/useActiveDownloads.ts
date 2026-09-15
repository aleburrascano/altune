import { useActiveDownloadItems, type DownloadEntry } from './downloadStore';

export function useActiveDownloads(): DownloadEntry[] {
  return useActiveDownloadItems();
}
