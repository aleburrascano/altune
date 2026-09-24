import { useId, type ReactNode } from "react";
import type { Snapshot } from "../types";
import { RelativeTime } from "./RelativeTime";
import { StateBadge } from "./StateBadge";

export function Panel({
  title,
  snapshot,
  actions,
  children,
}: {
  title: string;
  snapshot: Snapshot;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const headingId = useId();
  return (
    <article
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col gap-4 rounded-lg border border-border bg-bg-elev p-4"
    >
      <header className="flex flex-wrap items-center gap-3">
        <h2 id={headingId} className="m-0 min-w-0 flex-1 truncate text-base font-semibold text-fg">
          {title}
        </h2>
        <StateBadge state={snapshot.state} severity={snapshot.severity} reason={snapshot.reason} />
        <RelativeTime at={snapshot.updatedAt} />
        {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
      </header>
      <div className="flex min-w-0 flex-col gap-4">{children}</div>
      <footer className="font-mono text-xs text-fg-faint">{snapshot.id}</footer>
    </article>
  );
}

export function Section({ title, children }: { title: string; children: ReactNode }) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="flex min-w-0 flex-col gap-2">
      <h3 id={headingId} className="m-0 text-xs font-medium uppercase tracking-wider text-fg-dim">
        {title}
      </h3>
      {children}
    </section>
  );
}
