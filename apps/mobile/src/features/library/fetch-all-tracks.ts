/**
 * fetchAllTracks — the whole library, however many pages that takes.
 *
 * The Library screen is not a paginated list: Albums and Artists are grouped
 * client-side, and search/sort run over the full set (`library-feature` concept
 * doc). A partial page would therefore not render as "page 1 of N" — it would
 * silently render *wrong* lenses, with albums missing and counts understated.
 *
 * So this pages to completion rather than exposing pagination. The server clamps
 * `limit` at 2000 and reports `has_more`, which the old single `limit: 2000`
 * request ignored — a library past that cap was quietly truncated.
 */
import { getTracks } from '@shared/api-client/tracks';
import type { ListTracksResponse } from '@shared/api-client/types';

/** Server clamps at 2000; asking for exactly that minimises round-trips. */
export const TRACKS_PAGE_SIZE = 2000;

/** Hard stop so a server that always answers `has_more: true` can't spin
 *  forever. At 2000/page this is 100k tracks — far past any real library. */
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
    // An empty page with has_more still set would otherwise loop without
    // advancing the offset — stop on no progress, not just on has_more.
    if (!next.has_more || next.items.length === 0) break;
  }

  return { ...first, items, limit: items.length, offset: 0, has_more: false };
}
