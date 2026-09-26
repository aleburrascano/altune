import type { DiscoveryResult } from '@shared/api-client/discovery';

import { trackExtras } from '../extras-accessors';

import type { DetailFact } from './DetailFacts';
import { formatRuntime } from './formatters';

function tracksFact(tracks: readonly DiscoveryResult[]): DetailFact | null {
  return tracks.length > 0 ? { label: 'Tracks', value: String(tracks.length) } : null;
}

function runtimeSecondsOf(tracks: readonly DiscoveryResult[]): number {
  return tracks.reduce((sum, t) => sum + (trackExtras(t.extras).durationSeconds ?? 0), 0);
}

function runtimeFact(tracks: readonly DiscoveryResult[]): DetailFact | null {
  const runtime = formatRuntime(runtimeSecondsOf(tracks));
  return runtime !== null ? { label: 'Runtime', value: runtime } : null;
}

function releasedFact(year: string | null): DetailFact | null {
  return year !== null ? { label: 'Released', value: year } : null;
}

export function buildAlbumFacts(
  tracks: readonly DiscoveryResult[],
  year: string | null,
): (DetailFact | null)[] {
  return [tracksFact(tracks), runtimeFact(tracks), releasedFact(year)];
}
