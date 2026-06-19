/**
 * Typed client for the discovery endpoints.
 *
 * Slice 43 of discover-music-v1. Mirrors the wire shape in
 * docs/specs/discover-music-v1/spec.md §3.7.
 */

import { apiFetch } from './index';

export type DiscoveryKind = 'artist' | 'album' | 'track';
export type DiscoveryConfidence = 'high' | 'medium' | 'low';
export type DiscoveryProviderStatus =
  | 'ok'
  | 'timeout'
  | 'error'
  | 'rate_limited'
  | 'circuit_open';

export type DiscoverySource = {
  provider: string;
  external_id: string;
  url: string;
};

export type DiscoveryResult = {
  kind: DiscoveryKind;
  title: string;
  subtitle: string | null;
  image_url: string | null;
  confidence: DiscoveryConfidence;
  sources: DiscoverySource[];
  extras: Record<string, unknown>;
};

export type DiscoveryProviderInfo = {
  provider: string;
  status: DiscoveryProviderStatus;
  result_count: number;
  latency_ms: number;
};

export type RelatedGroup = {
  relationship: string;
  related_to: string;
  items: DiscoveryResult[];
};

export type DiscoverySearchResponse = {
  query: string;
  query_norm: string;
  results: DiscoveryResult[];
  providers: DiscoveryProviderInfo[];
  partial: boolean;
  cache: { hit: boolean; fetched_at: string | null };
  corrected_query?: string;
  original_query?: string;
  related?: RelatedGroup[];
};

export type DiscoverySuggestion = {
  text: string;
  kind: string;
  popularity: number;
};

export type DiscoverySuggestResponse = {
  suggestions: DiscoverySuggestion[];
};

export type SearchHistoryItem = {
  query: string;
  query_norm: string;
  executed_at: string;
};

export type DiscoverySearchHistoryResponse = {
  items: SearchHistoryItem[];
  total: number;
};

export type ClickPayload = {
  query_norm: string;
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null;
  position: number;
  confidence: DiscoveryConfidence;
};

export async function searchDiscovery(params: {
  q: string;
  kinds?: DiscoveryKind[];
  limit?: number;
  saveHistory?: boolean;
}): Promise<DiscoverySearchResponse> {
  const qs = new URLSearchParams({ q: params.q });
  if (params.kinds && params.kinds.length > 0) {
    qs.set('kinds', params.kinds.join(','));
  }
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  if (params.saveHistory === false) {
    qs.set('save_history', 'false');
  }
  return apiFetch<DiscoverySearchResponse>(`/v1/discovery/search?${qs.toString()}`);
}

export async function suggestDiscovery(params: {
  q: string;
  limit?: number;
}): Promise<DiscoverySuggestResponse> {
  const qs = new URLSearchParams({ q: params.q });
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  return apiFetch<DiscoverySuggestResponse>(`/v1/discovery/suggest?${qs.toString()}`);
}

export async function listSearchHistory(params?: {
  limit?: number;
}): Promise<DiscoverySearchHistoryResponse> {
  const qs = new URLSearchParams();
  if (params?.limit !== undefined) {
    qs.set('limit', String(params.limit));
  }
  const query = qs.toString();
  return apiFetch<DiscoverySearchHistoryResponse>(
    `/v1/discovery/search-history${query ? `?${query}` : ''}`,
  );
}

export async function recordClick(payload: ClickPayload): Promise<void> {
  await apiFetch<void>('/v1/discovery/clicks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
}

// --- Catalog browse (AC#14-20) ---

export type ContentFetchResponse = {
  items: DiscoveryResult[];
  provider: string;
  status: DiscoveryProviderStatus;
  latency_ms: number;
};

function _contentUrl(basePath: string, limit?: number): string {
  if (limit === undefined) return basePath;
  return `${basePath}?limit=${limit}`;
}

export async function getAlbumTracks(
  provider: string,
  externalId: string,
  limit?: number,
): Promise<ContentFetchResponse> {
  return apiFetch<ContentFetchResponse>(
    _contentUrl(`/v1/discovery/albums/${provider}/${encodeURIComponent(externalId)}/tracks`, limit),
  );
}

export async function getArtistTopTracks(
  provider: string,
  externalId: string,
  limit?: number,
): Promise<ContentFetchResponse> {
  return apiFetch<ContentFetchResponse>(
    _contentUrl(`/v1/discovery/artists/${provider}/${encodeURIComponent(externalId)}/top-tracks`, limit),
  );
}

export async function getArtistAlbums(
  provider: string,
  externalId: string,
  limit?: number,
  artistName?: string,
): Promise<ContentFetchResponse> {
  let url = _contentUrl(`/v1/discovery/artists/${provider}/${encodeURIComponent(externalId)}/albums`, limit);
  if (artistName) {
    url += `&name=${encodeURIComponent(artistName)}`;
  }
  return apiFetch<ContentFetchResponse>(url);
}
