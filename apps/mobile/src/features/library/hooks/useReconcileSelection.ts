import { useEffect } from 'react';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import type { Selection } from './useSelection';

type Actions = Pick<Selection, 'clear' | 'selectAll'>;

function survivors(idsKey: string, tracks: TrackResponse[]): [TrackId[], TrackId[]] {
  const live = new Set<string>(tracks.map((t) => t.id));
  const current = idsKey.split('\n') as TrackId[];
  return [current, current.filter((id) => live.has(id))];
}

function reconcile(idsKey: string, tracks: TrackResponse[], actions: Actions): void {
  if (idsKey === '') return;
  const [current, kept] = survivors(idsKey, tracks);
  if (kept.length === current.length) return;
  if (kept.length === 0) actions.clear();
  else actions.selectAll(kept);
}

export function useReconcileSelection(selection: Selection, tracks: TrackResponse[]): void {
  const { ids, clear, selectAll } = selection;
  const idsKey = ids.join('\n');
  useEffect(() => {
    reconcile(idsKey, tracks, { clear, selectAll });
  }, [idsKey, tracks, clear, selectAll]);
}
