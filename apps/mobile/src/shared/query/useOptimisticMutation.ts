import { Alert } from 'react-native';
import { useMutation, useQueryClient, type QueryKey } from '@tanstack/react-query';

import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';

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
  /**
   * Undoes only this mutation's own delta on a failed request. It receives the cache as it is
   * now, which may carry writes (SSE patches, refetches) that landed while the request was in
   * flight, so it must keep those rather than restoring `previous` wholesale.
   */
  revertOptimistic: (current: TCache, variables: TVariables, previous: TCache) => TCache;
};

type UnguardedOptions<TData, TVariables, TCache> = BaseOptions<TData, TVariables> & {
  /**
   * Keeps the pre-helper `useFavorites` behavior: no cancelQueries, an optimistic write even
   * against a cold cache, a rollback that restores the snapshot wholesale, and fire-and-forget
   * invalidation. Exists only so the extraction stays behavior-preserving; do not use it for
   * new mutations.
   */
  unguarded: true;
  applyOptimistic: (previous: TCache | undefined, variables: TVariables) => TCache;
};

type OptimisticMutationOptions<TData, TVariables, TCache> =
  | GuardedOptions<TData, TVariables, TCache>
  | UnguardedOptions<TData, TVariables, TCache>;

/** `epoch` is the session the mutation started under; see the late-callback fence below. */
type Snapshot<TCache> = { previous: TCache | undefined; epoch: number };

/**
 * One react-query mutation with an optimistic cache write: cancel in-flight fetches ->
 * snapshot -> optimistic write -> revert own delta on error -> optional alert -> invalidate.
 *
 * Rollback and settle are fenced by the session epoch: a request that finishes after its
 * user is gone would otherwise write their snapshot into the next user's cache.
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
    const epoch = currentSessionEpoch();
    if (options.unguarded) {
      queryClient.setQueryData(queryKey, options.applyOptimistic(previous, variables));
      return { previous, epoch };
    }
    if (previous) {
      queryClient.setQueryData(queryKey, options.applyOptimistic(previous, variables));
    }
    return { previous, epoch };
  };

  const rollback = (variables: TVariables, context: Snapshot<TCache> | undefined): void => {
    if (!isSameSession(context?.epoch)) return;
    if (options.unguarded) {
      queryClient.setQueryData(queryKey, context?.previous);
      return;
    }
    const previous = context?.previous;
    if (!previous) return;
    queryClient.setQueryData<TCache>(queryKey, (current) =>
      current ? options.revertOptimistic(current, variables, previous) : current,
    );
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
      rollback(variables, context);
      if (alertOnError) {
        const { title, message } = alertOnError(variables);
        Alert.alert(title, message);
      }
    },
    onSettled: (_data, _error, variables, context) => {
      if (!isSameSession(context?.epoch)) return undefined;
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
