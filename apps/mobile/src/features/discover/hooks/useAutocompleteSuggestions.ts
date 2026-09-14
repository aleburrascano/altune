import { useQuery } from '@tanstack/react-query';

import { suggestDiscovery } from '@shared/api-client/discovery';

import { discoveryKeys } from '@shared/lib/query-keys';
import { useReportQueryFailure } from '@shared/telemetry/useReportQueryFailure';

export function useAutocompleteSuggestions(inputValue: string) {
  const trimmed = inputValue.trim().toLowerCase();

  const { data, error } = useQuery({
    queryKey: discoveryKeys.suggest(trimmed),
    queryFn: () => suggestDiscovery({ q: trimmed, limit: 5 }),
    enabled: trimmed.length >= 2,
    staleTime: 60 * 1000,
  });

  useReportQueryFailure(error, 'suggest');

  return { data, error };
}
