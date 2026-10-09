import { useQueryClient, type MutationFunctionContext, type QueryKey } from '@tanstack/react-query';

import {
  onlyWhileTheStartingSessionLasts,
  pinStartingSession,
} from '@shared/session/guardedMutation';
import { isSameSession } from '@shared/session/sessionEpoch';
import { useAppMutation } from '@shared/query/useAppMutation';
import { showFailureAlert } from '@shared/ui';

type ErrorAlert = { title: string; message: string };

type BaseOptions<TData, TVariables> = {
  action: string;
  queryKey: QueryKey;
  mutationFn: (variables: TVariables) => Promise<TData>;
  invalidate?: (variables: TVariables) => readonly QueryKey[];
  alertOnError?: (variables: TVariables) => ErrorAlert;
  onSuccess?: (data: TData, variables: TVariables) => void;
};

type GuardedOptions<TData, TVariables, TCache> = BaseOptions<TData, TVariables> & {
  unguarded?: false;
  applyOptimistic: (previous: TCache, variables: TVariables) => TCache;
  revertOptimistic: (current: TCache, variables: TVariables, previous: TCache) => TCache;
};

type UnguardedOptions<TData, TVariables, TCache> = BaseOptions<TData, TVariables> & {
  unguarded: true;
  applyOptimistic: (previous: TCache | undefined, variables: TVariables) => TCache;
};

type OptimisticMutationOptions<TData, TVariables, TCache> =
  GuardedOptions<TData, TVariables, TCache> | UnguardedOptions<TData, TVariables, TCache>;

type Snapshot<TCache> = { previous: TCache | undefined; epoch: number };

type SuccessCallback<TData, TVariables, TCache> = (
  settled: TData,
  variables: TVariables,
  snapshot: Snapshot<TCache> | undefined,
  run: MutationFunctionContext,
) => void;

function onlyInTheStartingSession<TData, TVariables, TCache>(
  onSuccess: SuccessCallback<TData, TVariables, TCache>,
): SuccessCallback<TData, TVariables, TCache> {
  return (settled, variables, snapshot, run) => {
    if (isSameSession(snapshot?.epoch)) onSuccess(settled, variables, snapshot, run);
  };
}

function alertAfterRollback<TVariables>(
  action: string,
  alertOnError: ((variables: TVariables) => ErrorAlert) | undefined,
  variables: TVariables,
): void {
  if (!alertOnError) return;
  const { title, message } = alertOnError(variables);
  showFailureAlert({ surface: action, title, message });
}

export function useOptimisticMutation<TData, TVariables, TCache>(
  options: OptimisticMutationOptions<TData, TVariables, TCache>,
) {
  const queryClient = useQueryClient();
  const { queryKey, alertOnError } = options;
  const invalidationKeys = (variables: TVariables): readonly QueryKey[] =>
    options.invalidate?.(variables) ?? [queryKey];

  const writeOptimistic = (previous: TCache | undefined, variables: TVariables): void => {
    if (options.unguarded) {
      queryClient.setQueryData(queryKey, options.applyOptimistic(previous, variables));
      return;
    }
    if (previous) {
      queryClient.setQueryData(queryKey, options.applyOptimistic(previous, variables));
    }
  };

  const optimisticWrite = (variables: TVariables, epoch: number): Snapshot<TCache> => {
    const previous = queryClient.getQueryData<TCache>(queryKey);
    if (isSameSession(epoch)) writeOptimistic(previous, variables);
    return { previous, epoch };
  };

  const rollback = (variables: TVariables, context: Snapshot<TCache> | undefined): void => {
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

  return useAppMutation<TData, Error, TVariables, Snapshot<TCache>>({
    action: options.action,
    mutationFn: onlyWhileTheStartingSessionLasts(options.mutationFn),
    onMutate: options.unguarded
      ? (variables, run) => optimisticWrite(variables, pinStartingSession(run))
      : async (variables, run) => {
          const epoch = pinStartingSession(run);
          await queryClient.cancelQueries({ queryKey });
          return optimisticWrite(variables, epoch);
        },
    ...(options.onSuccess ? { onSuccess: onlyInTheStartingSession(options.onSuccess) } : {}),
    onError: (_error, variables, context) => {
      if (!isSameSession(context?.epoch)) return;
      rollback(variables, context);
      alertAfterRollback(options.action, alertOnError, variables);
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
