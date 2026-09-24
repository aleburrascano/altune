import { useRef } from "react";
import { observeElementRect, useVirtualizer, type Rect, type Virtualizer } from "@tanstack/react-virtual";
import { focusRing } from "../ui/focusRing";

export interface LogRow {
  time: string;
  level: string;
  msg: string;
  attrs: string | null;
  corrId?: string;
}

const ROW_HEIGHT = 24;
const VIEWPORT_HEIGHT = 320;

function observeRect(instance: Virtualizer<HTMLDivElement, Element>, cb: (rect: Rect) => void) {
  return observeElementRect(instance, (rect) => cb(rect.height > 0 ? rect : { ...rect, height: VIEWPORT_HEIGHT }));
}

export function LogTail({
  rows,
  empty,
  onCorrId,
}: {
  rows: LogRow[];
  empty: string;
  onCorrId?: (id: string) => void;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    observeElementRect: observeRect,
    initialRect: { width: 0, height: VIEWPORT_HEIGHT },
  });

  if (rows.length === 0) return <p className="text-sm text-fg/60">{empty}</p>;

  return (
    <div ref={scroller} className="h-80 overflow-auto border border-border font-mono text-xs">
      <ul className="relative m-0 list-none p-0" style={{ height: virtual.getTotalSize() }}>
        {virtual.getVirtualItems().map((item) => {
          const row = rows[item.index];
          return (
            <li
              key={item.key}
              className="absolute left-0 flex w-full gap-3 whitespace-nowrap px-2"
              style={{ height: item.size, transform: `translateY(${item.start}px)` }}
            >
              <span className="text-fg/60">{row.time}</span>
              <span className="w-12">{row.level}</span>
              <span>{row.msg}</span>
              {row.attrs && <span className="text-fg/60">{row.attrs}</span>}
              {row.corrId &&
                (onCorrId ? (
                  <button
                    type="button"
                    onClick={() => onCorrId(row.corrId as string)}
                    className={`border-0 bg-transparent p-0 font-mono text-accent hover:underline ${focusRing}`}
                  >
                    corr {row.corrId}
                  </button>
                ) : (
                  <span className="text-fg/60">corr {row.corrId}</span>
                ))}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
