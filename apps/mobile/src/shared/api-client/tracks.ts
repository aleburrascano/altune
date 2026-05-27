/**
 * Typed client for the catalog endpoints.
 */

import { apiFetch } from './index';
import type { ListTracksResponse } from './types';

export async function getTracks(params: {
  limit: number;
  offset: number;
}): Promise<ListTracksResponse> {
  const qs = new URLSearchParams({
    limit: String(params.limit),
    offset: String(params.offset),
  });
  return apiFetch<ListTracksResponse>(`/v1/tracks?${qs.toString()}`);
}
