import { useEffect, useState } from "react";

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

export function formatAge(atMs: number, nowMs: number): string {
  const ageMs = Math.max(0, nowMs - atMs);
  if (ageMs < MINUTE) return `${Math.floor(ageMs / SECOND)}s ago`;
  if (ageMs < HOUR) return `${Math.floor(ageMs / MINUTE)}m ago`;
  if (ageMs < DAY) return `${Math.floor(ageMs / HOUR)}h ago`;
  return `${Math.floor(ageMs / DAY)}d ago`;
}

function parseInstant(at: string): number | null {
  const atMs = Date.parse(at);
  return Number.isNaN(atMs) || atMs <= 0 ? null : atMs;
}

export function RelativeTime({ at }: { at: string }) {
  const atMs = parseInstant(at);
  if (atMs === null) return <span className="font-mono text-xs text-fg-faint">never</span>;
  return <TickingAge atMs={atMs} iso={at} />;
}

function TickingAge({ atMs, iso }: { atMs: number; iso: string }) {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    const timer = setInterval(() => setNowMs(Date.now()), SECOND);
    return () => clearInterval(timer);
  }, []);

  return (
    <time dateTime={iso} title={new Date(atMs).toLocaleString()} className="font-mono text-xs text-fg-faint">
      {formatAge(atMs, nowMs)}
    </time>
  );
}
