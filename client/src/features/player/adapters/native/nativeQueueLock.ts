import { warnPlayback } from '@features/player/core/playbackError';
import { withDeadline } from '@shared/timing';

const NATIVE_QUEUE_OP_TIMEOUT_MS = 15_000;

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
      warnPlayback('native queue timeout listener failed', {}, err);
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

function runWithinDeadline<T>(op: (fence: Fence) => Promise<T>): Promise<T> {
  const state: DeadlineState = { hasExpired: false };
  const pending = new Promise<T>((resolve) => resolve(op(fenceOf(state))));
  return withDeadline(pending, NATIVE_QUEUE_OP_TIMEOUT_MS, {
    onExpire: () => {
      state.hasExpired = true;
      notifyTimeoutListeners();
      return new NativeQueueTimeoutError(NATIVE_QUEUE_OP_TIMEOUT_MS);
    },
    onLate: (err) => warnPlayback('native queue operation failed after its deadline', {}, err),
  });
}

function advanceChain(): void {}

export function withNativeQueue<T>(op: (fence: Fence) => Promise<T>): Promise<T> {
  function start(): Promise<T> {
    return runWithinDeadline(op);
  }
  const run = chain.then(start, start);
  chain = run.then(advanceChain, advanceChain);
  return run;
}
