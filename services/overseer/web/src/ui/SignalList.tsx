import type { Signal } from "../types";
import { Notice } from "./Notice";
import { focusRing } from "./focusRing";

export type { Signal };

function clockTime(iso: string): string {
  const atMs = Date.parse(iso);
  if (Number.isNaN(atMs) || atMs <= 0) return iso || "—";
  return new Date(atMs).toLocaleTimeString();
}

export function SignalList({
  signals,
  empty,
  onCorrId,
}: {
  signals: Signal[];
  empty: string;
  onCorrId?: (id: string) => void;
}) {
  if (signals.length === 0) return <Notice kind="empty">{empty}</Notice>;
  return (
    <ul className="m-0 flex list-none flex-col p-0">
      {signals.map((signal, index) => (
        <li
          key={`${signal.at}-${index}`}
          className="grid grid-cols-[auto_1fr_auto] items-baseline gap-x-3 border-b border-border py-1.5 text-sm last:border-b-0"
        >
          <span className="font-mono text-xs text-accent">{signal.kind}</span>
          <span className="min-w-0 truncate text-fg" title={signal.text}>
            {signal.text}
          </span>
          <time dateTime={signal.at} className="font-mono text-xs text-fg-faint">
            {clockTime(signal.at)}
          </time>
          {signal.corrId ? <CorrId id={signal.corrId} onCorrId={onCorrId} /> : null}
        </li>
      ))}
    </ul>
  );
}

function CorrId({ id, onCorrId }: { id: string; onCorrId?: (id: string) => void }) {
  const label = `corr ${id}`;
  if (!onCorrId) return <span className="col-start-2 font-mono text-2xs text-fg-faint">{label}</span>;
  return (
    <button
      type="button"
      onClick={() => onCorrId(id)}
      className={`col-start-2 justify-self-start border-0 bg-transparent p-0 font-mono text-2xs text-accent hover:underline ${focusRing}`}
    >
      {label}
    </button>
  );
}
