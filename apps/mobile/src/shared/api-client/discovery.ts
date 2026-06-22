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
  albumTitle?: string,
  albumArtist?: string,
): Promise<ContentFetchResponse> {
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (albumTitle) params.set('title', albumTitle);
  if (albumArtist) params.set('artist', albumArtist);
  const qs = params.toString();
  const url = `/v1/discovery/albums/${provider}/${encodeURIComponent(externalId)}/tracks${qs ? `?${qs}` : ''}`;
  return apiFetch<ContentFetchResponse>(url);
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

/**
 * Related tracks for a single track, keyed by the track's external id.
 * SoundCloud-only today (`/tracks/{id}/related`); a non-SoundCloud provider
 * returns an empty set with `status: 'error'`.
 */
export async function getRelatedTracks(
  provider: string,
  externalId: string,
  limit?: number,
): Promise<ContentFetchResponse> {
  return apiFetch<ContentFetchResponse>(
    _contentUrl(`/v1/discovery/tracks/${provider}/${encodeURIComponent(externalId)}/related`, limit),
  );
}

// --- MusicBrainz detail-open enrichment (musicbrainz-enrichment spec) ---

/**
 * MusicBrainz-derived detail enrichment: curated genres, year, community
 * rating, release types, the cross-provider id bridge, and a resolved HD cover.
 * Collections are always present (never null). An unresolved entity returns an
 * empty payload (`mbid: ''`, empty lists).
 */
export type EnrichmentResponse = {
  mbid: string;
  genres: string[];
  year: number;
  rating: number;
  rating_votes: number;
  primary_type: string;
  secondary_types: string[];
  external_ids: Record<string, string>;
  artwork_url: string;
};

export async function getEnrichment(params: {
  kind: DiscoveryKind;
  title?: string | undefined;
  subtitle?: string | null | undefined;
  mbid?: string | undefined;
}): Promise<EnrichmentResponse> {
  const qs = new URLSearchParams({ kind: params.kind });
  if (params.title) qs.set('title', params.title);
  if (params.subtitle) qs.set('subtitle', params.subtitle);
  if (params.mbid) qs.set('mbid', params.mbid);
  return apiFetch<EnrichmentResponse>(`/v1/discovery/enrichment?${qs.toString()}`);
}

// --- Discogs detail-open album enrichment (docs/providers/discogs.md caps 3–6) ---

export type DiscogsCredit = { name: string; role: string };
export type DiscogsLabel = { name: string; catno: string };
export type DiscogsCompany = { name: string; role: string };
export type DiscogsCommunity = {
  have: number;
  want: number;
  rating: number;
  votes: number;
};

/**
 * Discogs-derived album enrichment: credits/personnel, styles (the sub-genre
 * layer MusicBrainz lacks), label + catalog, formats, companies, and community
 * demand/rating. Collections are always present (never null). An unresolved
 * album returns an empty payload (`master_id: 0`, empty lists).
 */
export type DiscogsEnrichmentResponse = {
  master_id: number;
  genres: string[];
  styles: string[];
  year: number;
  credits: DiscogsCredit[];
  labels: DiscogsLabel[];
  formats: string[];
  country: string;
  companies: DiscogsCompany[];
  community: DiscogsCommunity;
};

export async function getDiscogsEnrichment(params: {
  album: string;
  artist?: string | null | undefined;
}): Promise<DiscogsEnrichmentResponse> {
  const qs = new URLSearchParams({ album: params.album });
  if (params.artist) qs.set('artist', params.artist);
  return apiFetch<DiscogsEnrichmentResponse>(
    `/v1/discovery/enrichment/discogs?${qs.toString()}`,
  );
}

export type DiscogsLink = { label: string; url: string };

/**
 * Discogs-derived artist enrichment: biography, name history (real name,
 * aliases, name variations), group/member relationships, and external links.
 * Collections are always present (never null). An unresolved artist returns an
 * empty payload (`artist_id: 0`, empty fields).
 */
export type DiscogsArtistEnrichmentResponse = {
  artist_id: number;
  profile: string;
  real_name: string;
  aliases: string[];
  name_variations: string[];
  members: string[];
  groups: string[];
  links: DiscogsLink[];
};

export async function getDiscogsArtistEnrichment(params: {
  name: string;
}): Promise<DiscogsArtistEnrichmentResponse> {
  const qs = new URLSearchParams({ name: params.name });
  return apiFetch<DiscogsArtistEnrichmentResponse>(
    `/v1/discovery/enrichment/discogs/artist?${qs.toString()}`,
  );
}

// --- Last.fm detail-open enrichment (docs/providers/lastfm.md cap 3) ---

/**
 * Last.fm-derived enrichment: listen-based popularity (listeners/playcount —
 * the scrobble signal MusicBrainz and Discogs lack), weighted folksonomy tags,
 * a biography/blurb, similar artists (artist kind only), and the entity's MBID.
 * Collections are always present (never null). An unresolved entity returns an
 * empty payload (`mbid: ''`, zero counts, empty lists). Kind-dispatched from
 * `kind` + `title` + `subtitle`, mirroring the MusicBrainz enrichment endpoint.
 */
export type LastFmEnrichmentResponse = {
  mbid: string;
  listeners: number;
  playcount: number;
  tags: string[];
  bio: string;
  similar: string[];
  duration: number;
  album: string;
};

export async function getLastFmEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
}): Promise<LastFmEnrichmentResponse> {
  const qs = new URLSearchParams({ kind: params.kind, title: params.title });
  if (params.subtitle) qs.set('subtitle', params.subtitle);
  return apiFetch<LastFmEnrichmentResponse>(
    `/v1/discovery/enrichment/lastfm?${qs.toString()}`,
  );
}

// --- Deezer detail-open enrichment (docs/providers/deezer.md caps 7–8) ---

/**
 * Deezer-derived enrichment for a track or album: track audio fields (`bpm`,
 * `gain` — a volume-normalization value, not displayed — and the `explicit`
 * flag) and album liner data (`label`, `genres`, `upc` barcode, `record_type`).
 * An unresolved entity returns an empty payload (zero/empty fields). Kind-
 * dispatched from `kind` + `title` + `subtitle`; only track and album resolve
 * (artist returns empty). Lyrics are a separate feature, not part of this payload.
 */
export type DeezerEnrichmentResponse = {
  bpm: number;
  gain: number;
  explicit: boolean;
  label: string;
  genres: string[];
  upc: string;
  record_type: string;
};

export async function getDeezerEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
}): Promise<DeezerEnrichmentResponse> {
  const qs = new URLSearchParams({ kind: params.kind, title: params.title });
  if (params.subtitle) qs.set('subtitle', params.subtitle);
  return apiFetch<DeezerEnrichmentResponse>(
    `/v1/discovery/enrichment/deezer?${qs.toString()}`,
  );
}

// --- Deezer lyrics (docs/providers/deezer.md cap 6) ---

/**
 * One time-synced lyric line: the LRC-style timecode marker, the line text, and
 * the start offset + duration in milliseconds (for player-driven scrubbing).
 */
export type SyncedLyricLine = {
  timecode: string;
  line: string;
  milliseconds: number;
  duration: number;
};

/**
 * Deezer-derived lyrics for a track: the full plain text, the time-synced lines
 * (empty when only plain text exists), the songwriter credits, and the copyright
 * line. Lyrics are the one metadata axis no other provider carries. A track with
 * no lyrics (or none for this region) returns an empty payload (`plain: ''`,
 * `synced_lines: []`). Tracks only — there is no album/artist lyrics surface.
 */
export type LyricsResponse = {
  plain: string;
  synced_lines: SyncedLyricLine[];
  writers: string[];
  copyright: string;
};

export async function getLyrics(params: {
  title: string;
  subtitle?: string | null | undefined;
}): Promise<LyricsResponse> {
  const qs = new URLSearchParams({ title: params.title });
  if (params.subtitle) qs.set('subtitle', params.subtitle);
  return apiFetch<LyricsResponse>(`/v1/discovery/lyrics?${qs.toString()}`);
}
