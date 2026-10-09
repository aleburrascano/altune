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
  if (external?.aborted) relay();
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

export function startDeadline(external: AbortSignal | undefined, ms: number): Deadline {
  const controller = new AbortController();
  const { timer, expired } = armTimer(controller, ms);
  const release = releaser(timer, external, relayAbort(controller, external));
  return { signal: controller.signal, expired, cancelled: isCancelled(external), release };
}
