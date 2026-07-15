import type { FeaturedArtist } from '@shared/api-client/types';

/** Parse the `featured_artists` extras key on a discovery result. The wire shape
 * is an array of `{name, mbid?, deezer_id?}` objects; legacy payloads carried
 * bare name strings, mapped to id-less credits. Junk entries are dropped. */
export function featuredArtistsFromExtras(raw: unknown): FeaturedArtist[] {
  if (!Array.isArray(raw)) return [];
  const out: FeaturedArtist[] = [];
  for (const item of raw as unknown[]) {
    if (typeof item === 'string') {
      if (item.length > 0) out.push({ name: item, mbid: null, deezer_id: null });
      continue;
    }
    if (item !== null && typeof item === 'object') {
      const rec = item as Record<string, unknown>;
      const name = typeof rec['name'] === 'string' ? rec['name'] : '';
      if (name.length === 0) continue;
      out.push({
        name,
        mbid: typeof rec['mbid'] === 'string' ? rec['mbid'] : null,
        deezer_id: typeof rec['deezer_id'] === 'number' ? rec['deezer_id'] : null,
      });
    }
  }
  return out;
}

/** Comma-join a base artist with its collaborating/featured artists:
 * "Ken Carson, Playboi Carti". Returns the base unchanged when there are none.
 * Reads as a combined credit — fits the scene where guests are co-billed rather
 * than "feat."-ed, and stays short so it doesn't crowd out the album on a row. */
export function withFeaturing(base: string, featured: readonly FeaturedArtist[] | undefined): string {
  if (!featured || featured.length === 0) return base;
  return [base, ...featured.map((f) => f.name)].join(', ');
}
