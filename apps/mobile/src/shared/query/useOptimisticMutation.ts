import { Alert } from 'react-native';
import { useMutation, useQueryClient, type QueryKey } from '@tanstack/react-query';

type ErrorAlert = { title: string; message: string };

type BaseOptions<TData, TVariables> = {
  /** The cache entry that is snapshotted, optimistically written and rolled back. */
  queryKey: QueryKey;
  mutationFn: (variables: TVariables) => Promise<TData>;
  /** Keys refetched once the mutation settles. Defaults to `[queryKey]`. */
  invalidate?: (variables: TVariables) => readonly QueryKey[];
  /** Alert shown after a failed mutation has been rolled back. Omit for a silent failure. */
  alertOnError?: (variables: TVariables) => ErrorAlert;
  onSuccess?: (data: TData, variables: TVariables) => void;
};

type GuardedOptions<TData, TVariables, TCache> = BaseOptions<TData, TVariables> & {
  unguarded?: false;
  /** Called only when the cache holds a snapshot; a cold cache is left untouched. */
  applyOptimistic: (previous: TCache, variables: TVariables) => TCache;
};

type UnguardedOptions<TData, TVariables, TCache> = BaseOptions<TData, TVariables> & {
  /**
   * Keeps the pre-helper `useFavorites` behavior: no cancelQueries, an optimistic write even
   * against a cold cache, an unconditional rollback, and fire-and-forget invalidation. Exists
   * only so the extraction stays behavior-preserving; do not use it for new mutations.
   */
  unguarded: true;
  applyOptimistic: (previous: TCache | undefined, variables: TVariables) => TCache;
};

type OptimisticMutationOptions<TData, TVariables, TCache> =
  | GuardedOptions<TData, TVariables, TCache>
  | UnguardedOptions<TData, TVariables, TCache>;

type Snapshot<TCache> = { previous: TCache | undefined };

/**
 * One react-query mutation with an optimistic cache write: cancel in-flight fetches ->
 * snapshot -> optimistic write -> rollback on error -> optional alert -> invalidate.
 */
export function useOptimisticMutation<TData, TVariables, TCache>(
  options: OptimisticMutationOptions<TData, TVariables, TCache>,
) {
  const queryClient = useQueryClient();
  const { queryKey, alertOnError } = options;
  const invalidationKeys = (variables: TVariables): readonly QueryKey[] =>
    options.invalidate?.(variables) ?? [queryKey];

  const optimisticWrite = (variables: TVariables): Snapshot<TCache> => {
    const previous = queryClient.getQueryData<TCache>(queryKey);
    if (options.unguarded) {
      queryClient.setQueryData(queryKey, options.applyOptimistic(previous, variables));
      return { previous };
    }
    if (previous) {
      queryClient.setQueryData(queryKey, options.applyOptimistic(previous, variables));
    }
    return { previous };
  };

  return useMutation<TData, Error, TVariables, Snapshot<TCache>>({
    mutationFn: options.mutationFn,
    onMutate: options.unguarded
      ? optimisticWrite
      : async (variables) => {
          await queryClient.cancelQueries({ queryKey });
          return optimisticWrite(variables);
        },
    ...(options.onSuccess ? { onSuccess: options.onSuccess } : {}),
    onError: (_error, variables, context) => {
      if (options.unguarded || context?.previous) {
        queryClient.setQueryData(queryKey, context?.previous);
      }
      if (alertOnError) {
        const { title, message } = alertOnError(variables);
        Alert.alert(title, message);
      }
    },
    onSettled: (_data, _error, variables) => {
      const invalidations = invalidationKeys(variables).map((key) =>
        queryClient.invalidateQueries({ queryKey: key }),
      );
      if (options.unguarded) {
        void Promise.all(invalidations);
        return undefined;
      }
      return Promise.all(invalidations);
    },
  });
}
