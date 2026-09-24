import { useEffect } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import type { Selection } from './useSelection';

export function useReconcileSelection(selection: Selection, tracks: TrackResponse[]): void {
  const { ids, clear, selectAll } = selection;
  const idsKey = ids.join('\n');

  useEffect(() => {
    if (idsKey === '') return;
    const live = new Set<string>(tracks.map((t) => t.id));
    const current = idsKey.split('\n') as TrackId[];
    const kept = current.filter((id) => live.has(id));
    if (kept.length === current.length) return;
    if (kept.length === 0) clear();
    else selectAll(kept);
  }, [idsKey, tracks, clear, selectAll]);
}
