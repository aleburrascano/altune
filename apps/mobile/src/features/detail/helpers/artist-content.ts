import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';
import { albumExtras } from '../extras-accessors';
import { normalizeForDedup } from './normalize-for-dedup';

export function getReleaseSortKey(album: DiscoveryResult): string | null {
  const ae = albumExtras(album.extras);
  return ae.releaseDate ?? ae.year;
}

export function sortByReleaseDateDesc(albums: DiscoveryResult[]): DiscoveryResult[] {
  return [...albums].sort((a, b) => {
    const dateA = getReleaseSortKey(a);
    const dateB = getReleaseSortKey(b);
    if (dateA === null) return 1;
    if (dateB === null) return -1;
    return dateB.localeCompare(dateA);
  });
}

export function mergedSources(a: DiscoverySource[], b: DiscoverySource[]): DiscoverySource[] {
  const seen = new Set(a.map((s) => `${s.provider}:${s.external_id}`));
  const merged = [...a];
  for (const s of b) {
    const key = `${s.provider}:${s.external_id}`;
    if (!seen.has(key)) {
      seen.add(key);
      merged.push(s);
    }
  }
  return merged;
}

export function dedupAlbumsByTitle(albums: DiscoveryResult[]): DiscoveryResult[] {
  const groups = new Map<string, DiscoveryResult>();
  for (const album of albums) {
    const key = normalizeForDedup(album.title);
    const existing = groups.get(key);
    if (existing === undefined) {
      groups.set(key, album);
    } else {
      const existingCount = albumExtras(existing.extras).trackCount ?? 0;
      const newCount = albumExtras(album.extras).trackCount ?? 0;
      const merged = mergedSources(existing.sources, album.sources);
      if (newCount > existingCount) {
        groups.set(key, { ...album, sources: merged });
      } else {
        groups.set(key, { ...existing, sources: merged });
      }
    }
  }
  return Array.from(groups.values());
}
