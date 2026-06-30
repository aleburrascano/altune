/**
 * Reactive list of in-flight downloads for the Activity Dock.
 *
 * Reads the shared `['library-home']` cache with `enabled: false` — it never
 * fetches from the dock (no startup cost), but re-renders when the cache
 * changes (SSE patches, optimistic saves, or the library screen's own fetch).
 * So the dock reflects pending tracks once the library cache is populated.
 */

import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';

import { deriveActiveDownloads, type DownloadItem } from './activeDownloads';

export function useActiveDownloads(): DownloadItem[] {
  const { data } = useQuery({
    queryKey: ['library-home'],
    queryFn: () => getTracks({ limit: 2000, offset: 0 }),
    enabled: false,
  });
  return deriveActiveDownloads(data, undefined);
}
