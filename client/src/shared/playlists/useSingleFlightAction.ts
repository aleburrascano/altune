import { useCallback, useEffect, useRef, useState } from 'react';

type SingleFlightActionOptions<T> = {
  open: boolean;
  resolve: () => Promise<T[]>;
  onResolveError?: (error: unknown) => void;
  onClose: () => void;
};

type SingleFlightAction<T> = {
  resolving: boolean;
  run: (dispatch: (items: T[]) => void) => Promise<void>;
  close: () => void;
  closeAfter: (delayMs: number, beforeClose: () => void) => void;
  cancelScheduledClose: () => void;
};

export function useSingleFlightAction<T>({
  open,
  resolve,
  onResolveError,
  onClose,
}: SingleFlightActionOptions<T>): SingleFlightAction<T> {
  const [resolving, setResolving] = useState(false);
  const dispatchedRef = useRef(false);
  const closedRef = useRef(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancelScheduledClose = useCallback(() => {
    if (closeTimer.current) {
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }, []);
  const close = useCallback(() => {
    if (closedRef.current) return;
    closedRef.current = true;
    onClose();
  }, [onClose]);
  useEffect(() => cancelScheduledClose, [cancelScheduledClose]);
  useEffect(() => {
    if (!open) {
      dispatchedRef.current = false;
      closedRef.current = false;
    }
  }, [open]);

  const closeAfter = useCallback(
    (delayMs: number, beforeClose: () => void) => {
      cancelScheduledClose();
      closeTimer.current = setTimeout(() => {
        closeTimer.current = null;
        beforeClose();
        close();
      }, delayMs);
    },
    [cancelScheduledClose, close],
  );

  const run = useCallback(
    async (dispatch: (items: T[]) => void): Promise<void> => {
      if (dispatchedRef.current) return;
      dispatchedRef.current = true;
      setResolving(true);
      try {
        const items = await resolve();
        if (items.length > 0) {
          dispatchedRef.current = false;
          dispatch(items);
        }
      } catch (error) {
        onResolveError?.(error);
        close();
      } finally {
        setResolving(false);
      }
    },
    [close, onResolveError, resolve],
  );

  return { resolving, run, close, closeAfter, cancelScheduledClose };
}
