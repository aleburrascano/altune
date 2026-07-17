import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { resolveEntityQuery } from '../resolve-entity-query';

const norm = (s: string): string => s.toLowerCase().trim();

/**
 * Back-fill `sources` for a library item (a saved track/album/artist has no
 * provider sources stored) so it becomes playable and can fetch album tracklists
 * / artist discography.
 *
 * NON-DESTRUCTIVE by design: the stored library metadata is the source of truth.
 * We search title + artist (never title alone — "Green Day" would match an
 * unrelated release), accept a match only when BOTH agree, and merge so the
 * library's own `extras` win on every conflict — enrichment may only *add*
 * provider-only keys (mbid, preview_url) and the `sources`, never overwrite the
 * album/artist we already know. A loose title-only match used to swap the real
 * album out from under the user (e.g. "Green Day" by Che).
 */
export function useEnrichResult(result: DiscoveryResult): {
  enriched: DiscoveryResult;
  isEnriching: boolean;
} {
  const needsEnrichment = result.sources.length === 0;
  const searchTerm = result.subtitle ? `${result.title} ${result.subtitle}` : result.title;

  const { data } = useQuery({
    ...resolveEntityQuery(result.kind, searchTerm, 5),
    enabled: needsEnrichment,
  });

  if (!needsEnrichment) {
    return { enriched: result, isEnriching: false };
  }

  if (!data?.length) {
    return { enriched: result, isEnriching: !data };
  }

  const titleNorm = norm(result.title);
  const artistNorm = result.subtitle ? norm(result.subtitle) : null;
  // Require the title to match AND — when we know the artist — the artist too.
  // No fuzzy fallback: a wrong match is worse than no enrichment (we keep the
  // stored metadata either way; we'd only be borrowing sources we can't trust).
  const match =
    data.find(
      (r) =>
        r.kind === result.kind &&
        norm(r.title) === titleNorm &&
        (artistNorm === null || (r.subtitle != null && norm(r.subtitle) === artistNorm)),
    ) ?? null;

  if (!match || match.sources.length === 0) {
    return { enriched: result, isEnriching: false };
  }

  return {
    // result spread last → the library's stored `extras` (album, artist,
    // track_position, …) win on every conflict; we only gain `sources` plus any
    // provider-only keys the library didn't have.
    enriched: { ...result, sources: match.sources, extras: { ...match.extras, ...result.extras } },
    isEnriching: false,
  };
}
