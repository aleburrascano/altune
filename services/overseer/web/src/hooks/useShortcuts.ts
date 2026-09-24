import { useEffect } from "react";

export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable;
}

function isPaletteChord(event: KeyboardEvent): boolean {
  return (
    (event.metaKey || event.ctrlKey) &&
    !event.shiftKey &&
    !event.altKey &&
    event.key.toLowerCase() === "k"
  );
}

function isPlainKey(event: KeyboardEvent): boolean {
  return !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey;
}

export interface BucketShortcuts {
  openPalette: () => void;
  nextBucket: () => void;
  previousBucket: () => void;
}

export function useShortcuts({ openPalette, nextBucket, previousBucket }: BucketShortcuts): void {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (isTypingTarget(event.target)) return;

      if (isPaletteChord(event)) {
        event.preventDefault();
        openPalette();
        return;
      }

      if (!isPlainKey(event)) return;

      const key = event.key.toLowerCase();
      if (key === "j" || key === "]") {
        nextBucket();
      } else if (key === "k" || key === "[") {
        previousBucket();
      }
    }

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [openPalette, nextBucket, previousBucket]);
}
