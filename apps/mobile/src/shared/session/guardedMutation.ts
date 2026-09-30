import type {
  DefaultError,
  MutationFunctionContext,
  UseMutationOptions,
} from '@tanstack/react-query';

import { currentSessionEpoch, isSameSession } from '@shared/session/sessionEpoch';

const startingSession = new WeakMap<MutationFunctionContext, number>();

export function pinStartingSession(run: MutationFunctionContext): number {
  const epoch = currentSessionEpoch();
  startingSession.set(run, epoch);
  return epoch;
}

export class SessionEndedError extends Error {
  constructor() {
    super('the session that started this mutation has ended');
    this.name = 'SessionEndedError';
  }
}

export function onlyWhileTheStartingSessionLasts<TData, TVariables>(
  mutationFn: (variables: TVariables) => Promise<TData>,
) {
  return (variables: TVariables, run: MutationFunctionContext): Promise<TData> => {
    if (!isSameSession(startingSession.get(run))) {
      return Promise.reject(new SessionEndedError());
    }
    return mutationFn(variables);
  };
}

type SessionFenced<TContext> = TContext & { epoch: number };

type GuardedMutation<TData, TVariables, TContext extends object> = {
  mutationFn: (variables: TVariables) => Promise<TData>;
  onMutate?: (variables: TVariables) => TContext;
  onSuccess?: (
    data: TData,
    variables: TVariables,
    context: SessionFenced<TContext>,
  ) => Promise<unknown> | unknown;
  onError?: (
    error: DefaultError,
    variables: TVariables,
    context: SessionFenced<TContext>,
  ) => Promise<unknown> | unknown;
};

export function guardedMutationOptions<
  TData,
  TVariables = void,
  TContext extends object = Record<string, never>,
>({
  mutationFn,
  onMutate,
  onSuccess,
  onError,
}: GuardedMutation<TData, TVariables, TContext>): UseMutationOptions<
  TData,
  DefaultError,
  TVariables,
  SessionFenced<TContext>
> {
  return {
    mutationFn: onlyWhileTheStartingSessionLasts(mutationFn),
    onMutate: (variables, run) => {
      const epoch = pinStartingSession(run);
      const context = onMutate?.(variables) ?? ({} as TContext);
      return { ...context, epoch };
    },
    ...(onSuccess ? { onSuccess: onlyInTheSameSession(onSuccess) } : {}),
    ...(onError ? { onError: onlyInTheSameSession(onError) } : {}),
  };
}

function onlyInTheSameSession<TSettleArg, TVariables, TContext>(
  settle: (
    settleArg: TSettleArg,
    variables: TVariables,
    context: SessionFenced<TContext>,
  ) => Promise<unknown> | unknown,
) {
  return (
    settleArg: TSettleArg,
    variables: TVariables,
    context: SessionFenced<TContext> | undefined,
  ) => {
    if (!context || !isSameSession(context.epoch)) return undefined;
    return settle(settleArg, variables, context);
  };
}
