/**
 * Reactive list of in-flight downloads for the Activity Dock.
 *
 * Membership is now driven entirely by the SSE-fed download lifecycle store
 * (keyed by track_id), NOT by deriving `acquisition_status === 'pending'` from
 * the library cache. That single source of truth is what fixes the "row vanishes
 * the instant it completes" bug — a cache status flip no longer changes dock
 * membership. Thin re-export so callers keep a stable import site.
 */

import { useActiveDownloadItems, type DownloadEntry } from './downloadStore';

export function useActiveDownloads(): DownloadEntry[] {
  return useActiveDownloadItems();
}

export type { DownloadEntry as DownloadItem };
