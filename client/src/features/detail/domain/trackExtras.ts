import { parseTrackId, type TrackId } from '@shared/apiClient/ids';
import type { AcquisitionStatus, FeaturedArtist } from '@shared/apiClient/trackResponse';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { featuredArtistsFromExtras } from '@shared/apiClient/featuredArtists';

export type TrackExtras = {
  durationSeconds: number | null;
  album: string | null;
  isrc: string | null;
  year: number | null;
  genre: string | null;
  albumArtist: string | null;
  featuredArtists: FeaturedArtist[];
  trackId: TrackId | null;
  acquisitionStatus: AcquisitionStatus | null;
  previewUrl: string | null;
  mbid: string | null;
  trackPosition: number | null;
};

type DiscoveryExtras = DiscoveryResult['extras'];

function trackIdOrNull(value: string): TrackId | null {
  const parsed = parseTrackId(value);
  return parsed.ok ? parsed.id : null;
}

function asNonEmptyText(value: unknown): string | null {
  return typeof value === 'string' && value.length > 0 ? value : null;
}

function asText(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

function asFiniteNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

function asDuration(value: unknown): number | null {
  const seconds = asFiniteNumber(value);
  return seconds !== null && seconds >= 0 ? seconds : null;
}

function asAcquisitionStatus(value: unknown): AcquisitionStatus | null {
  return value === 'ready' || value === 'pending' || value === 'failed' ? value : null;
}

function asTrackId(value: unknown): TrackId | null {
  return typeof value === 'string' ? trackIdOrNull(value) : null;
}

function trackFacts(extras: DiscoveryExtras) {
  return {
    durationSeconds: asDuration(extras['duration'] ?? extras['duration_seconds']),
    album: asNonEmptyText(extras['album']),
    isrc: asNonEmptyText(extras['isrc']),
    year: asFiniteNumber(extras['year']),
    genre: asNonEmptyText(extras['genre']),
    albumArtist: asNonEmptyText(extras['album_artist']),
  };
}

function ownedAcquisitionStatus(extras: DiscoveryExtras): AcquisitionStatus | null {
  return asAcquisitionStatus(extras['acquisition_status'] ?? extras['owned_acquisition_status']);
}

function trackOwnership(extras: DiscoveryExtras) {
  return {
    featuredArtists: featuredArtistsFromExtras(extras['featured_artists']),
    trackId: asTrackId(extras['track_id'] ?? extras['owned_track_id']),
    acquisitionStatus: ownedAcquisitionStatus(extras),
    previewUrl: asNonEmptyText(extras['preview_url']),
    mbid: asText(extras['mbid']),
    trackPosition: asFiniteNumber(extras['track_position']),
  };
}

export function trackExtras(extras: DiscoveryExtras): TrackExtras {
  return { ...trackFacts(extras), ...trackOwnership(extras) };
}

export type AlbumExtrasResult = {
  releaseDate: string | null;
  year: string | null;
  trackCount: number | null;
  recordType: string | null;
};

function asYearText(value: unknown): string | null {
  return typeof value === 'number' ? String(value) : asText(value);
}

export function albumExtras(extras: DiscoveryExtras): AlbumExtrasResult {
  const trackCount = extras['track_count'];
  return {
    releaseDate: asNonEmptyText(extras['release_date']),
    year: asYearText(extras['year']),
    trackCount: typeof trackCount === 'number' ? trackCount : null,
    recordType: asText(extras['record_type']),
  };
}

const _FEAT_RE =
  /(?:[([]\s*(?:feat\.?|ft\.?|featuring|with)|\b(?:feat\.?|ft\.?|featuring))\s+([^)\]]+?)(?:[)\]]|$)/i;

export function extractFeaturedFromText(title: string, subtitle: string | null): string | null {
  for (const text of [title, subtitle ?? '']) {
    const match = _FEAT_RE.exec(text);
    if (match?.[1]) {
      return match[1].trim();
    }
  }
  return null;
}

export function resolveFeatured(
  extras: DiscoveryExtras,
  deezerFeatured: FeaturedArtist[] | undefined,
  title: string,
  subtitle: string | null,
): FeaturedArtist[] {
  const structured = trackExtras(extras).featuredArtists;
  if (structured.length > 0) {
    return structured;
  }
  if (deezerFeatured && deezerFeatured.length > 0) {
    return deezerFeatured;
  }
  const names = extractFeaturedFromText(title, subtitle)?.split(', ') ?? [];
  const out: FeaturedArtist[] = [];
  for (const raw of names) {
    const name = raw.trim();
    if (name.length === 0) continue;
    out.push({ name, mbid: null, deezer_id: null });
  }
  return out;
}
