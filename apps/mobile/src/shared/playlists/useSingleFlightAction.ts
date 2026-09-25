import { useCallback, useEffect, useRef, useState } from 'react';

type SingleFlightActionOptions<T> = {
  /** Whether the owning surface is open; closing it resets both guards. */
  open: boolean;
  /** Resolves the items one gesture acts on. A rejection closes the surface. */
  resolve: () => Promise<T[]>;
  /** Receives a `resolve` rejection, so the close it forces is distinguishable from a deliberate one. */
  onResolveError?: (error: unknown) => void;
  onClose: () => void;
};

type SingleFlightAction<T> = {
  /** True while `resolve` is in flight. */
  resolving: boolean;
  /** Runs one gesture: resolve the items, then hand a non-empty result to `dispatch`. */
  run: (dispatch: (items: T[]) => void) => Promise<void>;
  /** Idempotent close: fires onClose at most once per opening. */
  close: () => void;
  /** Schedules a close after `delayMs`, replacing any pending one; `beforeClose` runs first. */
  closeAfter: (delayMs: number, beforeClose: () => void) => void;
  cancelScheduledClose: () => void;
};

/**
 * Dedupes one gesture and one close per opening of a sheet.
 *
 * Under React 19.2 (Expo 57) the concurrent renderer can re-dispatch one press
 * a moment later, during the interaction's settle window, and can re-run a
 * rejected continuation. A disabled-while-busy UI alone can't dedupe that: when
 * the resolve yields items the caller's mutation starts and its pending state
 * keeps the control disabled, so the replay is harmlessly swallowed there — but
 * on the empty/reject paths nothing runs, busy clears, and the replay used to
 * re-run `resolve()`/`onClose()`. So the dispatch lock engages on entry and is
 * only released once `dispatch` has taken over the gesture; an empty or
 * rejected resolve keeps it engaged (there is nothing to act on), which drops
 * the replay. Every close routes through an idempotent guard, so a repeat is
 * dropped. Both guards clear when `open` goes false.
 */
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
          // The dispatched work now owns the gesture; its pending state guards
          // the control against a replayed press, so hand the lock back.
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
