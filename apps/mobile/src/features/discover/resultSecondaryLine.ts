import { featuredArtistsFromExtras, withFeaturing } from '@shared/lib/featured';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { kindLabel } from './kindLabel';

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
