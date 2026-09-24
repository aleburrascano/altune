import { Link, useParams, useSearchParams } from "react-router-dom";
import * as ToggleGroup from "@radix-ui/react-toggle-group";
import type { Range, Snapshot } from "../types";
import { panelFor } from "../panels/registry";
import { overviewPath, parseRange } from "../routes";
import { focusRing } from "../ui/focusRing";

// BucketDetail is the drill-down view: the full panel for one bucket, resolved by
// id through the registry (a bespoke panel or the generic fallback), with a way back
// to the overview and a 1h/24h/7d range kept in the URL. It updates live because the
// parent feeds it the merged snapshot map. If the id has no snapshot yet (still
// loading, or an unknown deep link) it shows the back-link and a clean notice rather
// than crashing.
export function BucketDetail({ snapshots }: { snapshots: Record<string, Snapshot> }) {
  const { id = "" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const range = parseRange(searchParams.get("range"));
  const snapshot = snapshots[id];
  const Panel = panelFor(id);

  function setRange(next: Range) {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.set("range", next);
        return params;
      },
      { replace: true },
    );
  }

  return (
    <div className="detail w-full">
      <div className="detail-bar flex flex-wrap items-center justify-between gap-3">
        <Link to={overviewPath} className="back-link">
          <span aria-hidden="true">←</span> Overview
        </Link>
        {snapshot ? <RangePicker range={range} onChange={setRange} /> : null}
      </div>
      {snapshot ? (
        <div className="w-full min-w-0">
          <Panel snapshot={snapshot} range={range} />
        </div>
      ) : (
        <p className="empty">No data for bucket “{id}” yet.</p>
      )}
    </div>
  );
}

const RANGE_OPTIONS: { value: Range; label: string }[] = [
  { value: "1h", label: "1h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
];

function RangePicker({ range, onChange }: { range: Range; onChange: (r: Range) => void }) {
  return (
    <ToggleGroup.Root
      type="single"
      value={range}
      onValueChange={(v) => {
        if (v) onChange(v as Range);
      }}
      aria-label="time range"
      className="inline-flex gap-1 rounded-md border border-border bg-bg-elev p-0.5"
    >
      {RANGE_OPTIONS.map((opt) => (
        <ToggleGroup.Item
          key={opt.value}
          value={opt.value}
          className={`rounded px-2.5 py-1 text-xs font-medium text-fg-dim transition-colors hover:text-fg data-[state=on]:bg-bg-elev-2 data-[state=on]:text-fg ${focusRing}`}
        >
          {opt.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  );
}
