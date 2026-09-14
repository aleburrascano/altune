import type { DiscoveryResult } from '@shared/api-client/discovery';

import { extractFeaturedFromText } from '../featured-artists';
import { albumExtras, trackExtras } from '../extras-accessors';

export function trackSubtitleWithFeaturing(track: DiscoveryResult): string {
  const base = track.subtitle ?? '';
  const names = trackExtras(track.extras).featuredArtists.map((f) => f.name);
  if (names.length > 0) return `${base}, ${names.join(', ')}`;
  const parsed = extractFeaturedFromText(track.title, track.subtitle);
  if (parsed) return `${base}, ${parsed}`;
  return base;
}

export function albumYear(album: DiscoveryResult): string | null {
  const ae = albumExtras(album.extras);
  if (ae.releaseDate != null) return ae.releaseDate.slice(0, 4);
  return ae.year;
}

export function compactCount(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

export function formatRuntime(totalSeconds: number): string | null {
  if (totalSeconds <= 0) return null;
  const totalMinutes = Math.floor(totalSeconds / 60);
  if (totalMinutes === 0) return '< 1 min';
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return hours > 0 ? `${hours} hr ${minutes} min` : `${minutes} min`;
}
