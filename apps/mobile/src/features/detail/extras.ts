import type { FeaturedArtist } from '@shared/api-client/types';

import { trackExtras } from './extras-accessors';

const _FEAT_RE = /(?:\(|\[)?\s*(?:feat\.?|ft\.?|featuring|with)\s+([^)\]]+?)(?:\)|\]|$)/i;

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
  extras: Record<string, unknown>,
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
  return (extractFeaturedFromText(title, subtitle)?.split(', ') ?? []).map((name) => ({
    name,
    mbid: null,
    deezer_id: null,
  }));
}
