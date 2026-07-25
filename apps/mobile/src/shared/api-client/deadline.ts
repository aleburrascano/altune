export interface Deadline {
  signal: AbortSignal;
  expired: () => boolean;
  cancelled: () => boolean;
  release: () => void;
}

export function startDeadline(external: AbortSignal | undefined, ms: number): Deadline {
  const controller = new AbortController();
  let expired = false;
  const timer = setTimeout(() => {
    expired = true;
    controller.abort();
  }, ms);
  const relay = (): void => controller.abort();

  if (external?.aborted) relay();
  else external?.addEventListener('abort', relay);

  return {
    signal: controller.signal,
    expired: () => expired,
    cancelled: () => external?.aborted === true,
    release: () => {
      clearTimeout(timer);
      external?.removeEventListener('abort', relay);
    },
  };
}
