import type { FeaturedArtist } from '@shared/catalog/core/trackResponse';

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
      const deezerId = rec['deezer_id'];
      out.push({
        name,
        mbid: typeof rec['mbid'] === 'string' ? rec['mbid'] : null,
        deezer_id: Number.isFinite(deezerId) ? (deezerId as number) : null,
      });
    }
  }
  return out;
}

export function withFeaturing(
  base: string,
  featured: readonly FeaturedArtist[] | undefined,
): string {
  if (!featured) return base;
  return [base, ...featured.map((f) => f.name)].join(', ');
}
