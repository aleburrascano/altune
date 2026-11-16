import type { FavoriteKey } from '@shared/catalog/core/ids';

export const DISCOVERY_KINDS = ['artist', 'album', 'track'] as const;
export const DISCOVERY_CONFIDENCES = ['high', 'medium', 'low'] as const;
export const PROVIDER_STATUSES = [
  'ok',
  'timeout',
  'error',
  'rate_limited',
  'circuit_open',
  'pending',
] as const;

export type DiscoveryKind = (typeof DISCOVERY_KINDS)[number];
export type DiscoveryConfidence = (typeof DISCOVERY_CONFIDENCES)[number];
export type DiscoveryProviderStatus = (typeof PROVIDER_STATUSES)[number];

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
  result_signature?: string | undefined;
  favorite_key?: FavoriteKey | undefined;
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

export type ResultSection = {
  kind: DiscoveryKind;
  items: DiscoveryResult[];
  has_more: boolean;
};

export type DiscoverySearchResponse = {
  query: string;
  query_norm: string;
  search_id?: string | undefined;
  results: DiscoveryResult[];
  top_result?: DiscoveryResult | undefined;
  sections: ResultSection[];
  providers: DiscoveryProviderInfo[];
  partial: boolean;
  cache: { hit: boolean; fetched_at: string | null };
  corrected_query?: string;
  original_query?: string;
  related?: RelatedGroup[];
  total: number;
  offset: number;
  has_more: boolean;
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
