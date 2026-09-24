import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";

// GenericPanel is the fallback for any bucket id without a bespoke panel: it
// renders the envelope's state and its raw data payload as formatted JSON. This is
// what makes "additive on the frontend" true — a new backend bucket appears
// immediately through this fallback, then gets upgraded to a bespoke panel later.
// The payload is rendered with React's text escaping (never dangerouslySetInnerHTML),
// which is the escaping invariant that moved off the Go html/template.
export function GenericPanel({ snapshot }: Pick<PanelProps, "snapshot">) {
  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>
      <pre className="panel-json">{JSON.stringify(snapshot.data, null, 2)}</pre>
      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

export function formatUpdated(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "never";
  return new Date(t).toLocaleString();
}
