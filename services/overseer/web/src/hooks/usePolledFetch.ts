import { useEffect } from "react";

export function usePolledFetch<T>(
  enabled: boolean,
  load: () => Promise<T>,
  onResult: (result: T) => void,
  onError: (err: unknown) => void,
  pollMs: number,
): void {
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    let seq = 0;
    const run = () => {
      const mine = ++seq;
      load().then(
        (result) => {
          if (active && mine === seq) onResult(result);
        },
        (err) => {
          if (active && mine === seq) onError(err);
        },
      );
    };
    run();
    const timer = setInterval(run, pollMs);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [enabled, load, onResult, onError, pollMs]);
}
