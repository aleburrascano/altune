import { useCallback, useState } from 'react';

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

/**
 * One Set holds the selection for the hook's lifetime and every operation mutates it, so a row
 * tap costs a single Set operation rather than a scan and a copy of everything already
 * selected — building a selection N taps at a time costs N, not N² (#1700).
 *
 * Leaving selection mode empties that Set, so an idle selection is always an empty one and
 * `ids`/`count`/`has` need no mode check to stay right.
 */
export function useSelection(): Selection {
  const [selected] = useState(() => new Set<TrackId>());
  const [active, setActive] = useState(false);
  const [, setRevision] = useState(0);

  // Mutating the Set leaves nothing for React to compare, so the revision is what moves and
  // what a render waits on.
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
