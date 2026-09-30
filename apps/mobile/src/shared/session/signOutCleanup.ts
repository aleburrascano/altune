import type {
  DefaultError,
  MutationFunctionContext,
  UseMutationOptions,
} from '@tanstack/react-query';

export type SignOutCleanup = () => void | Promise<void>;

export type IdentityListener = (userId: string | null) => void;

const cleanups = new Set<SignOutCleanup>();
let signedIn = false;
let sessionEpoch = 0;

export function onSignOut(cleanup: SignOutCleanup): () => void {
  cleanups.add(cleanup);
  return () => {
    cleanups.delete(cleanup);
  };
}

const identityListeners = new Set<IdentityListener>();

export function onIdentityChange(listener: IdentityListener): () => void {
  identityListeners.add(listener);
  return () => {
    identityListeners.delete(listener);
  };
}

export function notifyIdentityChange(userId: string | null): void {
  for (const listener of identityListeners) listener(userId);
  setSignedInUser(userId !== null);
}

export function runSignOutCleanups(): void {
  sessionEpoch += 1;
  for (const cleanup of cleanups) {
    try {
      void Promise.resolve(cleanup()).catch(warnCleanupFailed);
    } catch (error) {
      warnCleanupFailed(error);
    }
  }
}

const MAX_MESSAGE_LENGTH = 120;

function warnCleanupFailed(error: unknown): void {
  const name = error instanceof Error ? error.name : 'non-Error';
  const message = error instanceof Error ? error.message.slice(0, MAX_MESSAGE_LENGTH) : '';
  console.warn(`[session] sign-out cleanup failed: ${name} ${message}`.trimEnd());
}

export function hasSignedInUser(): boolean {
  return signedIn;
}

export function setSignedInUser(isSignedIn: boolean): void {
  signedIn = isSignedIn;
}

export function currentSessionEpoch(): number {
  return sessionEpoch;
}

export function isSameSession(epoch: number | undefined): boolean {
  return epoch === sessionEpoch;
}

const startingSession = new WeakMap<MutationFunctionContext, number>();

export class SessionEndedError extends Error {
  constructor() {
    super('the session that started this mutation has ended');
    this.name = 'SessionEndedError';
  }
}

function onlyWhileTheStartingSessionLasts<TData, TVariables>(
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
      const epoch = currentSessionEpoch();
      startingSession.set(run, epoch);
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
