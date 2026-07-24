import { getTracks } from '@shared/api-client/tracks';
import type { ListTracksResponse } from '@shared/api-client/types';

export const TRACKS_PAGE_SIZE = 2000;

const MAX_PAGES = 50;

export async function fetchAllTracks(): Promise<ListTracksResponse> {
  const first = await getTracks({ limit: TRACKS_PAGE_SIZE, offset: 0 });
  if (!first.has_more) return first;

  const items = [...first.items];
  let offset = items.length;

  for (let page = 1; page < MAX_PAGES; page += 1) {
    const next = await getTracks({ limit: TRACKS_PAGE_SIZE, offset });
    items.push(...next.items);
    offset += next.items.length;
    if (!next.has_more || next.items.length === 0) break;
  }

  return { ...first, items, limit: items.length, offset: 0, has_more: false };
}
