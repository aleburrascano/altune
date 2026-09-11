import { useEffect, useRef, useState } from 'react';
import type { ViewToken } from 'react-native';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import { buildImpressionRows } from '../impressions';
import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

export type ImpressionHandlers = {
  viewabilityConfig: { itemVisiblePercentThreshold: number };
  onViewableItemsChanged: (info: { viewableItems: ViewToken[] }) => void;
};

export function useImpressionLogger(
  searchData: DiscoverySearchResponse | undefined,
): ImpressionHandlers {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);
  const dataRef = useRef(searchData);
  const emittedFor = useRef<string | null>(null);

  // Keep the latest event recorder and search data reachable from the
  // viewability callback without reading them during render (react-hooks/refs).
  useEffect(() => {
    recordRef.current = recordEvent;
    dataRef.current = searchData;
  });

  const [handlers] = useState<ImpressionHandlers>(() => ({
    viewabilityConfig: { itemVisiblePercentThreshold: 50 },
    onViewableItemsChanged: ({ viewableItems }) => {
      const data = dataRef.current;
      const searchId = data?.search_id;
      if (!searchId || viewableItems.length === 0) return;
      if (emittedFor.current === searchId) return;
      const rows = buildImpressionRows(data.results);
      if (rows.length === 0) return;
      emittedFor.current = searchId;
      recordRef.current.mutate({
        type: 'results_shown',
        query_norm: data.query_norm,
        search_id: searchId,
        payload: { results: rows },
      });
    },
  }));

  return handlers;
}
