import { useCallback, useState, useEffect } from 'react';
import type { TrackId } from '@shared/apiClient/ids';
import type { TrackResponse } from '@shared/apiClient/trackResponse';

export type Selection = {
  active: boolean;
  ids: TrackId[];
  count: number;
  has: (id: TrackId) => boolean;
  begin: (id: TrackId) => void;
  toggle: (id: TrackId) => void;
  selectAll: (ids: TrackId[]) => void;
  clear: () => void;
};

export function useSelection(): Selection {
  const [selected] = useState(() => new Set<TrackId>());
  const [active, setActive] = useState(false);
  const [, setRevision] = useState(0);

  const markChanged = useCallback((nowActive: boolean) => {
    setActive(nowActive);
    setRevision((revision) => revision + 1);
  }, []);

  const toggle = useCallback(
    (id: TrackId) => {
      if (!selected.delete(id)) selected.add(id);
      markChanged(selected.size > 0);
    },
    [selected, markChanged],
  );

  const begin = useCallback(
    (id: TrackId) => {
      if (active) return;
      selected.add(id);
      markChanged(true);
    },
    [active, selected, markChanged],
  );

  const selectAll = useCallback(
    (ids: TrackId[]) => {
      selected.clear();
      for (const id of ids) selected.add(id);
      markChanged(true);
    },
    [selected, markChanged],
  );

  const clear = useCallback(() => {
    selected.clear();
    markChanged(false);
  }, [selected, markChanged]);

  return {
    active,
    ids: [...selected],
    count: selected.size,
    has: (id) => selected.has(id),
    begin,
    toggle,
    selectAll,
    clear,
  };
}

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
