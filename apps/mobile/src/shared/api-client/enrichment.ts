/**
 * Typed client for the discovery detail-open surface: catalog browse
 * (album tracklists, artist top tracks / albums, related tracks) and the
 * per-provider enrichment endpoints.
 *
 * Split out of discovery.ts (2026-07-17 discover structure audit F1):
 * discovery.ts holds the search surface every discover hook imports;
 * this file holds the detail-only surface, so a new provider enrichment
 * never churns the module the discover feature depends on. Wire shapes
 * follow the enrichment null-object contract: collections always present,
 * unresolved entity = empty payload.
 */

import { apiFetch } from './index';

import type {
  DiscoveryKind,
  DiscoveryProviderStatus,
  DiscoveryResult,
} from './discovery';

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
  // Build the query with URLSearchParams like getAlbumTracks — the old
  // `_contentUrl(...) + '&name='` concatenation produced `/albums&name=X`
  // (no `?`) whenever limit was omitted.
  const params = new URLSearchParams();
  if (limit !== undefined) params.set('limit', String(limit));
  if (artistName) params.set('name', artistName);
  const qs = params.toString();
  const url = `/v1/discovery/artists/${provider}/${encodeURIComponent(externalId)}/albums${qs ? `?${qs}` : ''}`;
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

/**
 * Build the `?kind=…&title=…[&subtitle=…]` query shared by the per-provider
 * (Last.fm, Deezer) detail-open enrichment endpoints.
 */
function kindTitleQs(kind: DiscoveryKind, title: string, subtitle?: string | null): string {
  const qs = new URLSearchParams({ kind, title });
  if (subtitle) qs.set('subtitle', subtitle);
  return qs.toString();
}

export async function getLastFmEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
}): Promise<LastFmEnrichmentResponse> {
  return apiFetch<LastFmEnrichmentResponse>(
    `/v1/discovery/enrichment/lastfm?${kindTitleQs(params.kind, params.title, params.subtitle)}`,
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
  /** Guest ("Featured"-role) contributors from the track detail fetch. Wire shape
   * is `[{name, mbid?, deezer_id?}]`; parse via featuredArtistsFromExtras. */
  featured_artists?: unknown[];
};

export async function getDeezerEnrichment(params: {
  kind: DiscoveryKind;
  title: string;
  subtitle?: string | null | undefined;
}): Promise<DeezerEnrichmentResponse> {
  return apiFetch<DeezerEnrichmentResponse>(
    `/v1/discovery/enrichment/deezer?${kindTitleQs(params.kind, params.title, params.subtitle)}`,
  );
}
