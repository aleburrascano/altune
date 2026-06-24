import type { AcquisitionStatus } from '@shared/api-client/types';

export type TrackExtras = {
  durationSeconds: number | null;
  album: string | null;
  isrc: string | null;
  year: number | null;
  genre: string | null;
  albumArtist: string | null;
  featuredArtists: string[];
  trackId: string | null;
  acquisitionStatus: AcquisitionStatus | null;
  previewUrl: string | null;
  mbid: string | null;
  trackPosition: number | null;
};

export function trackExtras(extras: Record<string, unknown>): TrackExtras {
  const duration = extras['duration_seconds'];
  const album = extras['album'];
  const isrc = extras['isrc'];
  const year = extras['year'];
  const genre = extras['genre'];
  const albumArtist = extras['album_artist'];
  const featured = extras['featured_artists'];
  const trackId = extras['track_id'];
  const status = extras['acquisition_status'];
  const preview = extras['preview_url'];
  const mbid = extras['mbid'];
  const trackPosition = extras['track_position'];

  return {
    durationSeconds: typeof duration === 'number' && Number.isFinite(duration) ? duration : null,
    album: typeof album === 'string' && album.length > 0 ? album : null,
    isrc: typeof isrc === 'string' && isrc.length > 0 ? isrc : null,
    year: typeof year === 'number' && Number.isFinite(year) ? year : null,
    genre: typeof genre === 'string' && genre.length > 0 ? genre : null,
    albumArtist: typeof albumArtist === 'string' && albumArtist.length > 0 ? albumArtist : null,
    featuredArtists: Array.isArray(featured)
      ? (featured as unknown[]).filter((n): n is string => typeof n === 'string' && n.length > 0)
      : [],
    trackId: typeof trackId === 'string' ? trackId : null,
    acquisitionStatus: typeof status === 'string' && (status === 'ready' || status === 'pending' || status === 'failed') ? status : null,
    previewUrl: typeof preview === 'string' && preview.length > 0 ? preview : null,
    mbid: typeof mbid === 'string' ? mbid : null,
    trackPosition:
      typeof trackPosition === 'number' && Number.isFinite(trackPosition) ? trackPosition : null,
  };
}

export type AlbumExtrasResult = {
  releaseDate: string | null;
  year: string | null;
  trackCount: number | null;
  recordType: string | null;
};

export function albumExtras(extras: Record<string, unknown>): AlbumExtrasResult {
  const releaseDate = extras['release_date'];
  const year = extras['year'];
  const trackCount = extras['track_count'];
  const recordType = extras['record_type'];

  return {
    releaseDate: typeof releaseDate === 'string' ? releaseDate : null,
    year: typeof year === 'number' ? String(year) : typeof year === 'string' ? year : null,
    trackCount: typeof trackCount === 'number' ? trackCount : null,
    recordType: typeof recordType === 'string' ? recordType : null,
  };
}
