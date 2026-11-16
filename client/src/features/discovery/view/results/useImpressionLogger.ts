import { useEffect, useRef, useState } from 'react';

import { useRecordEvent } from '@shared/telemetry';

import { buildImpressionRows } from '@features/discovery/domain/buildImpressionRows';
import type { DiscoverySearchResponse } from '@shared/catalog';

type ViewableItem = { key: string; isViewable: boolean };

type ViewableItemsChange = { viewableItems: readonly ViewableItem[] };

export type ImpressionHandlers = {
  viewabilityConfig: { itemVisiblePercentThreshold: number };
  onViewableItemsChanged: (info: ViewableItemsChange) => void;
};

type UnreportedSearch = { searchId: string; results: DiscoverySearchResponse['results'] };

function unreportedSearch(
  response: DiscoverySearchResponse | undefined,
  viewableItems: readonly ViewableItem[],
  emittedFor: string | null,
): UnreportedSearch | null {
  const searchId = response?.search_id;
  if (!searchId || viewableItems.length === 0) return null;
  return emittedFor === searchId ? null : { searchId, results: response.results };
}

export function useImpressionLogger(
  searchData: DiscoverySearchResponse | undefined,
): ImpressionHandlers {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);
  const dataRef = useRef(searchData);
  const emittedFor = useRef<string | null>(null);

  useEffect(() => {
    recordRef.current = recordEvent;
    dataRef.current = searchData;
  });

  const [handlers] = useState<ImpressionHandlers>(() => ({
    viewabilityConfig: { itemVisiblePercentThreshold: 50 },
    onViewableItemsChanged: ({ viewableItems }) => {
      const search = unreportedSearch(dataRef.current, viewableItems, emittedFor.current);
      if (search === null) return;
      const rows = buildImpressionRows(search.results);
      if (rows.length === 0) return;
      emittedFor.current = search.searchId;
      recordRef.current.mutate({
        type: 'results_shown',
        search_id: search.searchId,
        payload: { results: rows },
      });
    },
  }));

  return handlers;
}
