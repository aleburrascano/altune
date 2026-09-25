export interface Deadline {
  signal: AbortSignal;
  expired: () => boolean;
  cancelled: () => boolean;
  release: () => void;
}

function armTimer(
  controller: AbortController,
  ms: number,
): { timer: ReturnType<typeof setTimeout>; expired: () => boolean } {
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

export function startDeadline(external: AbortSignal | undefined, ms: number): Deadline {
  const controller = new AbortController();
  const { timer, expired } = armTimer(controller, ms);
  const relay = relayAbort(controller, external);
  return {
    signal: controller.signal,
    expired,
    cancelled: () => external?.aborted === true,
    release: () => {
      clearTimeout(timer);
      external?.removeEventListener('abort', relay);
    },
  };
}
