import { featuredArtistsFromExtras, withFeaturing } from '@shared/api-client/featuredArtists';
import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';

const KIND_LABELS: Record<DiscoveryKind, readonly [string, string]> = {
  artist: ['Artist', 'Artists'],
  album: ['Album', 'Albums'],
  track: ['Track', 'Tracks'],
};

export function kindLabel(kind: DiscoveryKind, opts?: { plural?: boolean }): string {
  return KIND_LABELS[kind][opts?.plural ? 1 : 0];
}

export function resultKey(result: DiscoveryResult, index: number): string {
  const source = result.sources[0];
  return `${result.kind}-${source?.provider ?? 'x'}-${source?.external_id || `${result.title}-${index}`}`;
}

export function resultSecondaryLine(result: DiscoveryResult): string {
  const kind = kindLabel(result.kind);

  if (result.kind === 'artist') {
    return kind;
  }
  if (result.kind === 'album') {
    const year = result.extras['year'];
    const parts = [kind];
    if (result.subtitle) parts.push(result.subtitle);
    if (typeof year === 'number' || typeof year === 'string') parts.push(String(year));
    return parts.join(' · ');
  }
  const parts = [kind];
  if (result.subtitle) {
    const guests = featuredArtistsFromExtras(result.extras['featured_artists']);
    parts.push(withFeaturing(result.subtitle, guests));
  }
  const count = result.extras['variant_count'];
  if (typeof count === 'number' && count > 1) {
    parts.push(`+${count - 1} version${count > 2 ? 's' : ''}`);
  }
  return parts.join(' · ');
}
