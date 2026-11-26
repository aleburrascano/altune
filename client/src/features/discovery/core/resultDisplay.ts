import { featuredArtistsFromExtras, withFeaturing } from '@shared/catalog';
import type { DiscoveryKind, DiscoveryResult } from '@shared/catalog';

const KIND_LABELS: Record<DiscoveryKind, readonly [string, string]> = {
  artist: ['Artist', 'Artists'],
  album: ['Album', 'Albums'],
  track: ['Track', 'Tracks'],
};

export function kindLabel(kind: DiscoveryKind, opts?: { plural?: boolean }): string {
  return KIND_LABELS[kind][opts?.plural === true ? 1 : 0];
}

export function resultKey(item: DiscoveryResult, index: number): string {
  const source = item.sources[0];
  const externalId = source?.external_id;
  const idPart =
    externalId === undefined || externalId === '' ? `${item.title}-${index}` : externalId;
  return `${item.kind}-${source?.provider ?? 'x'}-${idPart}`;
}

function hasSubtitle(subtitle: string | null): subtitle is string {
  return subtitle !== null && subtitle.length > 0;
}

export function resultSecondaryLine(item: DiscoveryResult): string {
  const kind = kindLabel(item.kind);

  if (item.kind === 'artist') {
    return kind;
  }
  if (item.kind === 'album') {
    const year = item.extras['year'];
    const parts = [kind];
    if (hasSubtitle(item.subtitle)) parts.push(item.subtitle);
    if (typeof year === 'number' || typeof year === 'string') parts.push(String(year));
    return parts.join(' · ');
  }
  const parts = [kind];
  if (hasSubtitle(item.subtitle)) {
    const guests = featuredArtistsFromExtras(item.extras['featured_artists']);
    parts.push(withFeaturing(item.subtitle, guests));
  }
  const count = item.extras['variant_count'];
  if (typeof count === 'number' && count > 1) {
    parts.push(`+${count - 1} version${count > 2 ? 's' : ''}`);
  }
  return parts.join(' · ');
}
