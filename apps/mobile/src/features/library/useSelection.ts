import { useCallback, useMemo, useState } from 'react';

import type { TrackId } from '@shared/api-client/ids';

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
  const [selected, setSelected] = useState<readonly TrackId[] | null>(null);

  const set = useMemo(() => new Set(selected ?? []), [selected]);

  const toggle = useCallback((id: TrackId) => {
    setSelected((current) => {
      if (current === null) return [id];
      if (!current.includes(id)) return [...current, id];
      const next = current.filter((v) => v !== id);
      return next.length === 0 ? null : next;
    });
  }, []);

  const begin = useCallback((id: TrackId) => {
    setSelected((current) => (current === null ? [id] : current));
  }, []);

  const selectAll = useCallback((ids: TrackId[]) => {
    setSelected(ids);
  }, []);

  const clear = useCallback(() => {
    setSelected(null);
  }, []);

  const ids = selected == null ? [] : [...selected];

  return {
    active: selected !== null,
    ids,
    count: ids.length,
    has: (id) => set.has(id),
    begin,
    toggle,
    selectAll,
    clear,
  };
}
