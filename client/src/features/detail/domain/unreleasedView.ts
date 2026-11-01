import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import { isAcquirable } from '@features/detail/domain/unreleasedTrack';

export type UnreleasedSort = 'popular' | 'best_quality' | 'most_versions' | 'newest';
export type UnreleasedWhere = 'all' | 'released' | 'unreleased';
export type UnreleasedWhat = 'all' | 'playable';

type Comparator = (a: UnreleasedTrack, b: UnreleasedTrack) => number;

function byNewest(a: UnreleasedTrack, b: UnreleasedTrack): number {
  return (b.release_date ?? '').localeCompare(a.release_date ?? '');
}

const COMPARATORS: Record<UnreleasedSort, Comparator | null> = {
  popular: null,
  best_quality: (a, b) => b.quality_rank - a.quality_rank,
  most_versions: (a, b) => b.versions.length - a.versions.length,
  newest: byNewest,
};

function matchesWhere(track: UnreleasedTrack, where: UnreleasedWhere): boolean {
  if (where === 'all') return true;
  return track.off_streaming === (where === 'unreleased');
}

function matchesWhat(track: UnreleasedTrack, what: UnreleasedWhat): boolean {
  return what === 'all' || isAcquirable(track);
}

export function unreleasedView(
  items: readonly UnreleasedTrack[],
  sort: UnreleasedSort,
  where: UnreleasedWhere,
  what: UnreleasedWhat,
): UnreleasedTrack[] {
  const kept = items.filter((t) => matchesWhere(t, where) && matchesWhat(t, what));
  const compare = COMPARATORS[sort];
  return compare === null ? kept : kept.sort(compare);
}
