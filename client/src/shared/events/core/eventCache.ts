export type CacheKey = readonly unknown[];

export interface CacheFilters {
  readonly queryKey: CacheKey;
}

export type CacheUpdater<T> = (prev: T | undefined) => T | undefined;

export interface CachedQuery {
  readonly queryKey: CacheKey;
  readonly state: { readonly fetchStatus: string };
}

export type CacheChange =
  | {
      readonly type: 'updated';
      readonly query: CachedQuery;
      readonly action: { readonly type: string; readonly manual?: boolean };
    }
  | {
      readonly type:
        | 'added'
        | 'removed'
        | 'observerAdded'
        | 'observerRemoved'
        | 'observerResultsUpdated'
        | 'observerOptionsUpdated';
      readonly query: CachedQuery;
    };

export interface CachedQueries {
  findAll(filters: CacheFilters): CachedQuery[];
  subscribe(listener: (change: CacheChange) => void): () => void;
}

export interface EventCache {
  invalidateQueries(filters: CacheFilters): Promise<void>;
  getQueryData(queryKey: CacheKey): unknown;
  setQueryData<T>(queryKey: CacheKey, updater: CacheUpdater<T>): unknown;
  getQueriesData<T>(filters: CacheFilters): [CacheKey, T | undefined][];
  setQueriesData<T>(filters: CacheFilters, updater: CacheUpdater<T>): unknown;
  getQueryCache(): CachedQueries;
}
