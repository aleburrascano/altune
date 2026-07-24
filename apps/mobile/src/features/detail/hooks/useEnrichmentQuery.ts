import { useQuery, type QueryKey } from '@tanstack/react-query';

const ENRICHMENT_STALE_TIME = 1000 * 60 * 60 * 24;

type EnrichmentQuery<T> = {
  queryKey: QueryKey;
  queryFn: () => Promise<T>;
  hasContent: (data: T) => boolean;
  enabled: boolean;
};

type EnrichmentQueryResult<T> = {
  value: T | null;
  isLoading: boolean;
  isError: boolean;
};

export function useEnrichmentQuery<T>({
  queryKey,
  queryFn,
  hasContent,
  enabled,
}: EnrichmentQuery<T>): EnrichmentQueryResult<T> {
  const { data, isLoading, isError } = useQuery({
    queryKey,
    queryFn,
    enabled,
    staleTime: ENRICHMENT_STALE_TIME,
  });

  return {
    value: data && hasContent(data) ? data : null,
    isLoading,
    isError,
  };
}
