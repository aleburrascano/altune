import { useEffect, useRef, useState } from 'react';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import { buildImpressionRows } from '../impressions';
import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

export type ImpressionHandlers = {
  viewabilityConfig: { itemVisiblePercentThreshold: number };
  onViewableItemsChanged: (info: { viewableItems: readonly unknown[] }) => void;
};

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
      const data = dataRef.current;
      const searchId = data?.search_id;
      if (!searchId || viewableItems.length === 0) return;
      if (emittedFor.current === searchId) return;
      const rows = buildImpressionRows(data.results);
      if (rows.length === 0) return;
      emittedFor.current = searchId;
      recordRef.current.mutate({
        type: 'results_shown',
        search_id: searchId,
        payload: { results: rows },
      });
    },
  }));

  return handlers;
}
