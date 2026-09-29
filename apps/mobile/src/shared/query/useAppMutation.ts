import {
  isCancelledError,
  MutationCache,
  QueryCache,
  useMutation,
  type UseMutationOptions,
  type UseMutationResult,
} from '@tanstack/react-query';

import { correlationIdOf, isTelemetryGated, ApiError } from '@shared/errors';
import { SessionEndedError } from '@shared/session/signOutCleanup';
import { reportClientError } from '@shared/telemetry/clientErrorReporting';
import { recordUserAction } from '@shared/telemetry/userTelemetry';

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
