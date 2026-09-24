import { formatDuration } from '@shared/lib/format';

import type { TrackDetailActions } from '../hooks/useTrackDetailActions';

import type { DetailFact } from './DetailFacts';

type FactsInput = Pick<TrackDetailActions, 'durationSeconds' | 'year' | 'source' | 'isPreview'>;

function lengthFact(durationSeconds: number | null): DetailFact | null {
  if (durationSeconds == null || durationSeconds <= 0) return null;
  return { label: 'Length', value: formatDuration(durationSeconds) };
}

function sourceFact(source: FactsInput['source'], isPreview: boolean): DetailFact | null {
  if (source === null) return null;
  if (isPreview) return { label: 'Source', value: 'Preview', tone: 'warning' };
  return { label: 'Source', value: 'Library' };
}

export function buildTrackFacts(input: FactsInput): (DetailFact | null)[] {
  return [
    lengthFact(input.durationSeconds),
    input.year !== null ? { label: 'Released', value: input.year } : null,
    sourceFact(input.source, input.isPreview),
  ];
}
