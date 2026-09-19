// Sign-out cleanup registry. Module-level state (zustand stores, native players,
// on-disk caches) outlives the React tree AuthGate unmounts on sign-out, so any
// slice that holds one user's data outside react-query registers a cleanup here.
// useSession runs every cleanup whenever the signed-in identity changes (sign-out,
// or a direct switch to another account) and tracks whether anyone is signed in,
// for code that runs outside React (e.g. the headless playback service).
//
// Register once at module load or app boot; a cleanup must be idempotent (several
// useSession instances may observe the same change) and must not throw — one that
// does is isolated so the remaining cleanups still run.

import type {
  DefaultError,
  MutationFunctionContext,
  UseMutationOptions,
} from '@tanstack/react-query';

export type SignOutCleanup = () => void | Promise<void>;

const cleanups = new Set<SignOutCleanup>();
let signedIn = false;
let sessionEpoch = 0;

/** Registers `cleanup` to run on every identity change; returns an unregister function. */
export function onSignOut(cleanup: SignOutCleanup): () => void {
  cleanups.add(cleanup);
  return () => {
    cleanups.delete(cleanup);
  };
}

export function runSignOutCleanups(): void {
  sessionEpoch += 1;
  for (const cleanup of cleanups) {
    try {
      void Promise.resolve(cleanup()).catch(() => undefined);
    } catch {
      // Isolated: a failing cleanup must not keep the next user's data from being cleared.
    }
  }
}

/** Whether a user is currently signed in, as last observed by useSession. */
export function hasSignedInUser(): boolean {
  return signedIn;
}

export function setSignedInUser(isSignedIn: boolean): void {
  signedIn = isSignedIn;
}

/**
 * Advances on every identity change (each runSignOutCleanups). A mutation captures
 * it when it starts; if it no longer matches, the user who started the mutation is
 * gone, so neither a further attempt nor a late callback may act in their name.
 */
export function currentSessionEpoch(): number {
  return sessionEpoch;
}

export function isSameSession(epoch: number | undefined): boolean {
  return epoch === sessionEpoch;
}

/**
 * The session each mutation run started in, keyed by the object react-query builds once
 * per run and hands to every attempt of it. A WeakMap rather than a field on the call
 * site's context because `mutationFn` is given the run, not the context, and because two
 * runs of one hook must not share a pin. A react-query that stopped reusing that object
 * would read as "unpinned", which the check below refuses: the fence fails closed.
 */
const startingSession = new WeakMap<MutationFunctionContext, number>();

class SessionEndedError extends Error {
  constructor() {
    super('the session that started this mutation has ended');
    this.name = 'SessionEndedError';
  }
}

/**
 * Every attempt re-derives its bearer token from whatever session is current when it is
 * sent, so a retry firing during the backoff after a sign-out would execute against the
 * next user's account (#1752). `isRetryable` rejects this error, so the mutation settles
 * at once rather than waiting out the remaining attempts.
 */
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

/** The call site's own mutation context, plus the session the mutation started in. */
type SessionFenced<TContext> = TContext & { epoch: number };

type GuardedMutation<TData, TVariables, TContext extends object> = {
  mutationFn: (variables: TVariables) => Promise<TData>;
  /** Runs before the request; what it returns reaches the settle callbacks below. */
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

/**
 * Mutation options that cannot act for a user who is already gone: the session epoch is
 * captured on mutate, then re-checked before every attempt, so a retry never re-fires
 * under the next user's bearer token (#1752), and before `onSuccess`/`onError`, so a
 * response arriving after a sign-out never writes one user's data into the next user's
 * query cache (#836). Spread into `useMutation` beside the call site's own options.
 */
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
