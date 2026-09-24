import type { SeriesPoint } from "../types";
import { formatAt } from "./uplot";

export interface UptimeStripProps {
  points: SeriesPoint[];
}

function barClasses(v: number): string {
  if (v >= 1) return "h-full bg-ok";
  if (v > 0) return "h-[55%] bg-warn";
  return "h-[30%] bg-critical";
}

export function UptimeStrip(props: UptimeStripProps) {
  const { points } = props;

  if (points.length === 0) {
    return <p className="empty">no uptime history for this range yet</p>;
  }

  return (
    <div aria-label="uptime strip" className="flex h-6 min-w-0 items-end gap-0.5">
      {points.map((p, i) => (
        <span
          key={`${p.at}-${i}`}
          title={`${p.v >= 1 ? "up" : p.v > 0 ? "degraded" : "down"} · ${formatAt(p.at)}`}
          className={`flex-1 rounded-sm ${barClasses(p.v)}`}
        />
      ))}
    </div>
  );
}
