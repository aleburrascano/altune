import { useEffect, useRef } from 'react';

import { useRecordEvent } from '@shared/telemetry/useRecordEvent';

import type {
  DiscoveryProviderInfo,
  DiscoveryProviderStatus,
  DiscoverySearchResponse,
} from '@shared/api-client/discovery';

// Records one search_degraded event each time a partial response is shown, so a
// client-visible degraded search is measurable. The merged response object is
// rebuilt every render, so dedupe on the search identity rather than the object.
export function useDegradedSearchTelemetry(
  searchData: DiscoverySearchResponse | undefined,
  resultsIncomplete: boolean,
): void {
  const recordEvent = useRecordEvent();
  const recordRef = useRef(recordEvent);
  const reportedFor = useRef<string | null>(null);

  useEffect(() => {
    recordRef.current = recordEvent;
  });

  const key =
    resultsIncomplete && searchData ? (searchData.search_id ?? searchData.query_norm) : null;

  useEffect(() => {
    if (key === null || searchData === undefined || reportedFor.current === key) return;
    reportedFor.current = key;
    recordRef.current.mutate({
      type: 'search_degraded',
      search_id: searchData.search_id,
      payload: {
        result_count: searchData.results.length,
        degraded_providers: degradedProviders(searchData.providers),
      },
    });
  }, [key, searchData]);
}

function degradedProviders(
  providers: DiscoveryProviderInfo[],
): { provider: string; status: DiscoveryProviderStatus }[] {
  const degraded: { provider: string; status: DiscoveryProviderStatus }[] = [];
  for (const { provider, status } of providers) {
    if (status !== 'ok') degraded.push({ provider, status });
  }
  return degraded;
}
