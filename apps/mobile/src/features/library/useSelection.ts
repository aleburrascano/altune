import { useCallback, useMemo, useState } from 'react';

export type Selection = {
  active: boolean;
  ids: string[];
  count: number;
  has: (id: string) => boolean;
  begin: (id: string) => void;
  toggle: (id: string) => void;
  selectAll: (ids: string[]) => void;
  clear: () => void;
};

export function useSelection(): Selection {
  const [selected, setSelected] = useState<readonly string[] | null>(null);

  const set = useMemo(() => new Set(selected ?? []), [selected]);

  const toggle = useCallback((id: string) => {
    setSelected((current) => {
      if (current === null) return [id];
      if (!current.includes(id)) return [...current, id];
      const next = current.filter((v) => v !== id);
      return next.length === 0 ? null : next;
    });
  }, []);

  const begin = useCallback((id: string) => {
    setSelected((current) => (current === null ? [id] : current));
  }, []);

  const selectAll = useCallback((ids: string[]) => {
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
