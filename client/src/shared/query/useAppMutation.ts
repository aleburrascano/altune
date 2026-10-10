import {
  isCancelledError,
  MutationCache,
  QueryCache,
  useMutation,
  useQueryClient,
  type MutationFunctionContext,
  type QueryKey,
  type UseMutationOptions,
  type UseMutationResult,
} from '@tanstack/react-query';

import { correlationIdOf, isTelemetryGated, ApiError } from '@shared/failure/errors';
import {
  onlyWhileTheStartingSessionLasts,
  pinStartingSession,
  SessionEndedError,
} from '@shared/session/guardedMutation';
import { isSameSession } from '@shared/session/signOutCleanup';
import { reportClientError } from '@shared/telemetry/clientErrorReporting';
import { recordUserAction } from '@shared/telemetry/userTelemetry';
import { showFailureAlert } from '@shared/ui';

export type AppMutationOptions<TData, TError extends Error, TVars, TCtx> = UseMutationOptions<
  TData,
  TError,
  TVars,
  TCtx
> & {
  action: string;
  trackIdOf?: (vars: TVars) => string | undefined;
};

function isExpectedFailure(error: unknown): boolean {
  return isCancelledError(error) || error instanceof SessionEndedError || isTelemetryGated(error);
}

function optionally<K extends string, V>(key: K, value: V | undefined): { [P in K]?: V } {
  return value === undefined ? {} : ({ [key]: value } as { [P in K]?: V });
}

function failureFields(failure: Error): Record<string, unknown> {
  return {
    ...optionally('status', failure instanceof ApiError ? failure.status : undefined),
    ...optionally('correlation_id', correlationIdOf(failure)),
    error: failure.message,
  };
}

function recordOutcome(action: string, trackId: string | undefined, failure?: Error): void {
  if (failure instanceof SessionEndedError) return;
  recordUserAction({
    action,
    outcome: failure ? 'failed' : 'succeeded',
    ...optionally('track_id', trackId),
    ...(failure ? failureFields(failure) : {}),
  });
}

type TrackIdOf<TVars> = ((vars: TVars) => string | undefined) | undefined;

function recordingOnSuccess<TData, TError extends Error, TVars, TCtx>(
  action: string,
  trackIdOf: TrackIdOf<TVars>,
  inner: UseMutationOptions<TData, TError, TVars, TCtx>['onSuccess'],
): NonNullable<UseMutationOptions<TData, TError, TVars, TCtx>['onSuccess']> {
  return (data, vars, ctx, run) => {
    recordOutcome(action, trackIdOf?.(vars));
    return inner?.(data, vars, ctx, run);
  };
}

function recordingOnError<TData, TError extends Error, TVars, TCtx>(
  action: string,
  trackIdOf: TrackIdOf<TVars>,
  inner: UseMutationOptions<TData, TError, TVars, TCtx>['onError'],
): NonNullable<UseMutationOptions<TData, TError, TVars, TCtx>['onError']> {
  return (error, vars, ctx, run) => {
    recordOutcome(action, trackIdOf?.(vars), error);
    return inner?.(error, vars, ctx, run);
  };
}

function recordingHandlers<TData, TError extends Error, TVars, TCtx>(
  action: string,
  trackIdOf: TrackIdOf<TVars>,
  options: UseMutationOptions<TData, TError, TVars, TCtx>,
): UseMutationOptions<TData, TError, TVars, TCtx> {
  return {
    onSuccess: recordingOnSuccess(action, trackIdOf, options.onSuccess),
    onError: recordingOnError(action, trackIdOf, options.onError),
  };
}

export function useAppMutation<TData, TError extends Error, TVars, TCtx>(
  options: AppMutationOptions<TData, TError, TVars, TCtx>,
): UseMutationResult<TData, TError, TVars, TCtx> {
  const { action, trackIdOf, ...rest } = options;
  return useMutation<TData, TError, TVars, TCtx>({
    ...rest,
    meta: { ...rest.meta, action },
    ...recordingHandlers(action, trackIdOf, rest),
  });
}

export function appQueryCache(): QueryCache {
  return new QueryCache({
    onError: (error) => {
      if (isExpectedFailure(error)) return;
      reportClientError(error, 'query');
    },
  });
}

export function appMutationCache(): MutationCache {
  return new MutationCache({
    onError: (error) => {
      if (isExpectedFailure(error)) return;
      reportClientError(error, 'mutation');
    },
  });
}

type ErrorAlert = { title: string; message: string };

type BaseOptions<TData, TVariables> = {
  action: string;
  queryKey: QueryKey;
  mutationFn: (variables: TVariables) => Promise<TData>;
  invalidate?: (variables: TVariables) => readonly QueryKey[];
  alertOnError?: (variables: TVariables) => ErrorAlert;
  onSuccess?: (data: TData, variables: TVariables) => void;
};

type OptimisticMutationOptions<TData, TVariables, TCache> = BaseOptions<TData, TVariables> & {
  applyOptimistic: (previous: TCache, variables: TVariables) => TCache;
  revertOptimistic: (current: TCache, variables: TVariables, previous: TCache) => TCache;
};

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
    const previous = context?.previous;
    if (!previous) return;
    queryClient.setQueryData<TCache>(queryKey, (current) =>
      current ? options.revertOptimistic(current, variables, previous) : current,
    );
  };

  return useAppMutation<TData, Error, TVariables, Snapshot<TCache>>({
    action: options.action,
    mutationFn: onlyWhileTheStartingSessionLasts(options.mutationFn),
    onMutate: async (variables, run) => {
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
      return Promise.all(
        invalidationKeys(variables).map((key) => queryClient.invalidateQueries({ queryKey: key })),
      );
    },
  });
}
