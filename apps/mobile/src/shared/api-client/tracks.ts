/**
 * Typed client for the catalog endpoints.
 *
 * STUB: GREEN commit implements the real fetch wiring. Currently returns an
 * empty page so the Jest tests fail meaningfully.
 */

import type { ListTracksResponse } from './types';

export async function getTracks(params: {
  limit: number;
  offset: number;
}): Promise<ListTracksResponse> {
  // STUB
  return {
    items: [],
    total: 0,
    limit: params.limit,
    offset: params.offset,
    has_more: false,
  };
}
