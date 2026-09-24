import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import type { Snapshot } from "../types";
import { bucketPath, overviewPath } from "../routes";
import { focusRing } from "../ui/focusRing";

const linkClass = `flex min-w-0 items-center gap-2.5 px-2.5 py-1.5 text-sm text-fg-dim hover:bg-bg-elev-2 hover:text-fg aria-[current=page]:bg-bg-elev-2 aria-[current=page]:text-fg ${focusRing}`;

function NavItem({
  to,
  end,
  label,
  mark,
  isCollapsed,
}: {
  to: string;
  end?: boolean;
  label: string;
  mark: ReactNode;
  isCollapsed: boolean;
}) {
  return (
    <li>
      <NavLink to={to} end={end} title={isCollapsed ? label : undefined} className={linkClass}>
        {mark}
        <span className={isCollapsed ? "sr-only" : "truncate"}>{label}</span>
      </NavLink>
    </li>
  );
}

export function Nav({ buckets, isCollapsed = false }: { buckets: Snapshot[]; isCollapsed?: boolean }) {
  return (
    <nav aria-label="Buckets" className="flex min-h-0 flex-1 flex-col">
      <ul className="m-0 flex min-h-0 flex-1 list-none flex-col gap-0.5 overflow-y-auto p-0.5">
        <NavItem
          to={overviewPath}
          end
          label="Overview"
          isCollapsed={isCollapsed}
          mark={
            <span aria-hidden="true" className="inline-flex w-2 shrink-0 justify-center text-2xs text-fg-faint">
              ▦
            </span>
          }
        />
        {buckets.map((s) => (
          <NavItem
            key={s.id}
            to={bucketPath(s.id)}
            label={s.title}
            isCollapsed={isCollapsed}
            mark={<span aria-hidden="true" className={`dot dot-sev-${s.severity}`} />}
          />
        ))}
      </ul>
    </nav>
  );
}
