/**
 * Derives the list of in-flight downloads (tracks still acquiring audio) from
 * the library caches. Pure so it's unit-testable; the reactive hook wraps it.
 */

import type { InfiniteData } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

export interface DownloadItem {
  id: string;
  title: string;
  artist: string;
  artworkUrl: string | null;
}

function toItem(t: TrackResponse): DownloadItem {
  return { id: t.id, title: t.title, artist: t.artist, artworkUrl: t.artwork_url };
}

export function deriveActiveDownloads(
  home: ListTracksResponse | undefined,
  infinite: InfiniteData<ListTracksResponse> | undefined,
): DownloadItem[] {
  const seen = new Set<string>();
  const out: DownloadItem[] = [];
  const consider = (t: TrackResponse): void => {
    if (t.acquisition_status !== 'pending' || seen.has(t.id)) return;
    seen.add(t.id);
    out.push(toItem(t));
  };
  home?.items.forEach(consider);
  infinite?.pages.forEach((page) => page.items.forEach(consider));
  return out;
}
