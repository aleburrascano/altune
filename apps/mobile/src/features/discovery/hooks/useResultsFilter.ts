import { useState, type Dispatch, type SetStateAction } from 'react';

import type { DiscoveryKind } from '@shared/api-client/discovery';

export type ResultsFilter = 'all' | DiscoveryKind;

type ResultsFilterState = {
  filter: ResultsFilter;
  setFilter: Dispatch<SetStateAction<ResultsFilter>>;
};

export function useResultsFilter(committedQuery: string): ResultsFilterState {
  const [filter, setFilter] = useState<ResultsFilter>('all');
  const [filterQuery, setFilterQuery] = useState(committedQuery);
  if (filterQuery !== committedQuery) {
    setFilterQuery(committedQuery);
    setFilter('all');
  }
  return { filter, setFilter };
}
