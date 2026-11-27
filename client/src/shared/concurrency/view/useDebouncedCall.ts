import { useCallback, useEffect, useRef } from 'react';

type DebouncedCall = {
  schedule: (run: () => void) => void;
  cancel: () => void;
};

export function useDebouncedCall(delayMs: number): DebouncedCall {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancel = useCallback((): void => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  }, []);

  const schedule = useCallback(
    (run: () => void): void => {
      cancel();
      timer.current = setTimeout(() => {
        timer.current = null;
        run();
      }, delayMs);
    },
    [cancel, delayMs],
  );

  useEffect(() => cancel, [cancel]);

  return { schedule, cancel };
}
