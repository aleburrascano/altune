import { useCallback, useEffect, useRef, useState } from "react";

function readParam(name: string): string {
  return new URLSearchParams(window.location.search).get(name) ?? "";
}

function writeParam(name: string, value: string): void {
  const url = new URL(window.location.href);
  if (value === "") url.searchParams.delete(name);
  else url.searchParams.set(name, value);
  window.history.replaceState(window.history.state, "", url);
}

export function useUrlParam(name: string): [string, (value: string) => void] {
  const [value, setValue] = useState(() => readParam(name));
  const set = useCallback(
    (next: string) => {
      setValue(next);
      writeParam(name, next);
    },
    [name],
  );
  return [value, set];
}

export function useDebounced<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    const timer = setTimeout(() => setSettled(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return settled;
}
