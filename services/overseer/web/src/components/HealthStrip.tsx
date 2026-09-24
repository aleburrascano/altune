import { useContext, useEffect, useState } from "react";
import { fetchHealth, TokensContext } from "../api";
import type { OverseerHealth, Severity, Snapshot } from "../types";
import { Notice, RelativeTime } from "../ui";

const POLL_MS = 15_000;

const SEVERITY_TEXT: Record<Severity, string> = {
  ok: "text-ok",
  warn: "text-warn",
  critical: "text-critical",
};

const SEVERITY_LABEL: Record<Severity, string> = {
  ok: "all clear",
  warn: "attention needed",
  critical: "critical",
};

function worstSeverity(snapshots: Snapshot[]): Severity {
  if (snapshots.some((s) => s.severity === "critical")) return "critical";
  if (snapshots.some((s) => s.severity === "warn")) return "warn";
  return "ok";
}

function countSeverity(snapshots: Snapshot[], severity: Severity): number {
  return snapshots.filter((s) => s.severity === severity).length;
}

function countStale(snapshots: Snapshot[]): number {
  return snapshots.filter((s) => s.state !== "live").length;
}

// HealthStrip is the overview's at-a-glance strip: worst severity across every
// bucket, how many are warn/critical/stale, the last collect cycle, and the
// overseer's own credential state — read as a "login problem", never confused
// with go-api itself being unreachable (a spent refresh token is our failure,
// not a dependency outage).
export function HealthStrip({ snapshots }: { snapshots: Snapshot[] }) {
  const tokens = useContext(TokensContext);
  const [health, setHealth] = useState<OverseerHealth | null>(null);

  useEffect(() => {
    if (!tokens) return;
    let active = true;
    const load = () =>
      fetchHealth(tokens).then(
        (h) => {
          if (active) setHealth(h);
        },
        () => undefined,
      );
    void load();
    const timer = setInterval(load, POLL_MS);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [tokens]);

  const worst = worstSeverity(snapshots);
  const credential = health?.credential;
  const credentialProblem = credential !== undefined && credential !== null && !credential.ok;

  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-bg-elev p-4">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        <span className={`font-mono text-sm font-semibold uppercase tracking-wider ${SEVERITY_TEXT[worst]}`}>
          {SEVERITY_LABEL[worst]}
        </span>
        <span className="text-xs text-fg-dim">{countSeverity(snapshots, "critical")} critical</span>
        <span className="text-xs text-fg-dim">{countSeverity(snapshots, "warn")} warn</span>
        <span className="text-xs text-fg-dim">{countStale(snapshots)} stale</span>
        <span className="text-xs text-fg-faint">
          last cycle {health?.lastCycle ? <RelativeTime at={health.lastCycle} /> : "unknown"}
        </span>
      </div>
      {credentialProblem ? (
        <Notice kind="down">
          Login problem: overseer's own credential is failing ({credential.consecutiveFailures} time
          {credential.consecutiveFailures === 1 ? "" : "s"} in a row), not a go-api outage.
        </Notice>
      ) : null}
    </div>
  );
}
