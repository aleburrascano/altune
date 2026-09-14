import { useState, type Dispatch, type SetStateAction } from 'react';

import type { ResultsFilter } from '../state';

type ResultsFilterState = {
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
};

/** Results filter that resets to "all" whenever a new query is committed. */
export function useResultsFilter(committedQuery: string): ResultsFilterState {
  const [filter, setFilter] = useState<ResultsFilter>('all');
  // Tracking the previous query in state (not a ref) keeps this an
  // adjust-state-during-render pattern rather than a ref access during render
  // (react-hooks/refs).
  const [filterQuery, setFilterQuery] = useState(committedQuery);
  if (filterQuery !== committedQuery) {
    setFilterQuery(committedQuery);
    setFilter('all');
  }
  return { filter, setFilter };
}
