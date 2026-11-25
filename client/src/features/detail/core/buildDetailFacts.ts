import type { DiscoveryResult } from '@shared/catalog';
import type { PlaybackSource } from '@shared/playback';
import { formatDuration, type TextTone } from '@shared/ui';

import { trackExtras } from '@features/detail/core/trackExtras';
import { formatRuntime } from '@features/detail/core/formatters';

export type DetailFact = {
  label: string;
  value: string;
  tone?: TextTone;
};

export type ArtistFactsInput = {
  releases: number;
  inLibrary: number;
};

function releasesFact(releases: number): DetailFact | null {
  return releases > 0 ? { label: 'Releases', value: String(releases) } : null;
}

function inLibraryFact(inLibrary: number): DetailFact | null {
  return inLibrary > 0 ? { label: 'In library', value: String(inLibrary) } : null;
}

export function buildArtistFacts(input: ArtistFactsInput): (DetailFact | null)[] {
  return [releasesFact(input.releases), inLibraryFact(input.inLibrary)];
}

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

export type TrackFactsInput = {
  durationSeconds: number | null;
  year: string | null;
  source: PlaybackSource | null;
  isPreview: boolean;
};

function lengthFact(durationSeconds: number | null): DetailFact | null {
  if (durationSeconds == null || durationSeconds <= 0) return null;
  return { label: 'Length', value: formatDuration(durationSeconds) };
}

function sourceFact(source: TrackFactsInput['source'], isPreview: boolean): DetailFact | null {
  if (source === null) return null;
  if (isPreview) return { label: 'Source', value: 'Preview', tone: 'warning' };
  return { label: 'Source', value: 'Library' };
}

export function buildTrackFacts(input: TrackFactsInput): (DetailFact | null)[] {
  return [
    lengthFact(input.durationSeconds),
    input.year !== null ? { label: 'Released', value: input.year } : null,
    sourceFact(input.source, input.isPreview),
  ];
}
