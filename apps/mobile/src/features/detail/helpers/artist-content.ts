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

// dedupeTracksByTitle merges top-tracks from multiple providers, keeping the
// first occurrence of each normalized title (caller orders by provider
// precedence — Deezer mainstream before SoundCloud underground).
export function dedupeTracksByTitle(tracks: DiscoveryResult[]): DiscoveryResult[] {
  const seen = new Set<string>();
  const out: DiscoveryResult[] = [];
  for (const t of tracks) {
    const key = normalizeForDedup(t.title);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(t);
  }
  return out;
}

/**
 * Back-fill artwork for albums with no image (e.g. SoundCloud sets) from a
 * title-matched album from another provider in the same merged list.
 */
export function backfillAlbumArt(albums: DiscoveryResult[]): DiscoveryResult[] {
  const artByTitle = new Map<string, string>();
  for (const a of albums) {
    if (a.image_url) {
      const key = normalizeForDedup(a.title);
      if (!artByTitle.has(key)) artByTitle.set(key, a.image_url);
    }
  }
  return albums.map((a) => {
    if (a.image_url) return a;
    const donor = artByTitle.get(normalizeForDedup(a.title));
    return donor ? { ...a, image_url: donor } : a;
  });
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
