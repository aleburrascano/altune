/**
 * useImpressionLogger — visibility-confirmed results_shown emission.
 *
 * IAB/Promoted.ai: an impression only counts once the results actually entered
 * the viewport — that visibility confirmation is the prerequisite for any
 * position-bias correction. We gate on FlatList's onViewableItemsChanged and
 * emit exactly ONE results_shown summary per search_id (ordered signatures +
 * position + provider + confidence), not one event per row. The handlers object
 * is stable across renders (RN forbids swapping onViewableItemsChanged on the
 * fly); the live searchData + recordEvent are read through refs.
 */

import { useRef } from 'react';
import type { ViewToken } from 'react-native';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import { buildImpressionRows } from '../impressions';
import type { DiscoverySearchResponse } from '../../../shared/api-client/discovery';

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
