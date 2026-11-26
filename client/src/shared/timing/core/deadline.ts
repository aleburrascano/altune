export interface Deadline {
  signal: AbortSignal;
  expired: () => boolean;
  cancelled: () => boolean;
  release: () => void;
}

interface ArmedTimer {
  timer: ReturnType<typeof setTimeout>;
  expired: () => boolean;
}

function armTimer(controller: AbortController, ms: number): ArmedTimer {
  let expired = false;
  const timer = setTimeout(() => {
    expired = true;
    controller.abort();
  }, ms);
  return { timer, expired: () => expired };
}

function relayAbort(controller: AbortController, external: AbortSignal | undefined): () => void {
  const relay = (): void => controller.abort();
  if (external?.aborted === true) relay();
  else external?.addEventListener('abort', relay);
  return relay;
}

function releaser(
  timer: ReturnType<typeof setTimeout>,
  external: AbortSignal | undefined,
  relay: () => void,
): () => void {
  return () => {
    clearTimeout(timer);
    external?.removeEventListener('abort', relay);
  };
}

function isCancelled(external: AbortSignal | undefined): () => boolean {
  return () => external?.aborted === true;
}

export interface WithDeadlineOptions {
  onExpire: () => Error;
  onLate?: (error: unknown) => void;
  signal?: AbortSignal;
}

function ignoreLateFailure(): void {}

export function withDeadline<T>(
  work: Promise<T>,
  ms: number,
  { onExpire, onLate = ignoreLateFailure, signal }: WithDeadlineOptions,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    let expired = false;
    const timer = setTimeout(() => {
      expired = true;
      reject(onExpire());
    }, ms);
    function stopTimer(): void {
      clearTimeout(timer);
    }
    function settle(): void {
      stopTimer();
      signal?.removeEventListener('abort', stopTimer);
    }
    if (signal?.aborted === true) stopTimer();
    else signal?.addEventListener('abort', stopTimer);
    work.then(
      (value) => {
        settle();
        resolve(value);
      },
      (error: unknown) => {
        settle();
        if (expired) onLate(error);
        else resolve(work);
      },
    );
  });
}

export function startDeadline(external: AbortSignal | undefined, ms: number): Deadline {
  const controller = new AbortController();
  const { timer, expired } = armTimer(controller, ms);
  const release = releaser(timer, external, relayAbort(controller, external));
  return { signal: controller.signal, expired, cancelled: isCancelled(external), release };
}
