export const NATIVE_QUEUE_OP_TIMEOUT_MS = 15_000;

export class NativeQueueTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback command timed out after ${timeoutMs / 1000}s`);
    this.name = 'NativeQueueTimeoutError';
  }
}

export type Fence = () => void;

let chain: Promise<unknown> = Promise.resolve();

const timeoutListeners = new Set<() => void>();

export function onNativeQueueTimeout(listener: () => void): () => void {
  timeoutListeners.add(listener);
  return () => timeoutListeners.delete(listener);
}

function notifyTimeoutListeners(): void {
  for (const listener of timeoutListeners) {
    try {
      listener();
    } catch (err) {
      console.warn('[playback] native queue timeout listener failed', err);
    }
  }
}

interface DeadlineState {
  hasExpired: boolean;
}

function fenceOf(state: DeadlineState): Fence {
  return () => {
    if (state.hasExpired) throw new NativeQueueTimeoutError(NATIVE_QUEUE_OP_TIMEOUT_MS);
  };
}

function deadlineTimer(state: DeadlineState, reject: (err: Error) => void) {
  return setTimeout(() => {
    state.hasExpired = true;
    reject(new NativeQueueTimeoutError(NATIVE_QUEUE_OP_TIMEOUT_MS));
    notifyTimeoutListeners();
  }, NATIVE_QUEUE_OP_TIMEOUT_MS);
}

function withDeadline<T>(op: (fence: Fence) => Promise<T>): Promise<T> {
  const state: DeadlineState = { hasExpired: false };
  let timer: ReturnType<typeof setTimeout> | undefined;
  const expired = new Promise<never>((_, reject) => {
    timer = deadlineTimer(state, reject);
  });
  const pending = new Promise<T>((resolve) => resolve(op(fenceOf(state))));
  pending.catch(() => undefined);
  return Promise.race([pending, expired]).finally(() => clearTimeout(timer));
}

export function withNativeQueue<T>(op: (fence: Fence) => Promise<T>): Promise<T> {
  const start = () => withDeadline(op);
  const run = chain.then(start, start);
  chain = run.catch(() => undefined);
  return run;
}
