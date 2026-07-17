import { useQuery } from '@tanstack/react-query';

import { suggestDiscovery } from '@shared/api-client/discovery';

import { discoveryKeys } from '../keys';

export function useAutocompleteSuggestions(inputValue: string) {
  const trimmed = inputValue.trim().toLowerCase();

  return useQuery({
    queryKey: discoveryKeys.suggest(trimmed),
    queryFn: () => suggestDiscovery({ q: trimmed, limit: 5 }),
    enabled: trimmed.length >= 2,
    staleTime: 60 * 1000,
  });
}
