import { useRef } from 'react';
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
  recordRef.current = recordEvent;
  const dataRef = useRef(searchData);
  dataRef.current = searchData;
  const emittedFor = useRef<string | null>(null);

  const handlers = useRef<ImpressionHandlers>({
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
  });

  return handlers.current;
}
