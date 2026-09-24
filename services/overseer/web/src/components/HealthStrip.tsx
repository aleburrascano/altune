import { useHealth } from "../hooks/useHealth";
import type { Severity, Snapshot } from "../types";
import { Notice, RelativeTime } from "../ui";

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

function worstSeverity(snapshots: Snapshot[], credentialProblem: boolean): Severity {
  if (snapshots.some((s) => s.severity === "critical")) return "critical";
  if (credentialProblem || snapshots.some((s) => s.severity === "warn")) return "warn";
  return "ok";
}

function countSeverity(snapshots: Snapshot[], severity: Severity): number {
  return snapshots.filter((s) => s.severity === severity).length;
}

function countStale(snapshots: Snapshot[]): number {
  return snapshots.filter((s) => s.state !== "live").length;
}

export function HealthStrip({ snapshots }: { snapshots: Snapshot[] }) {
  const health = useHealth();

  const credential = health?.credential;
  const credentialProblem = credential !== undefined && credential !== null && !credential.ok;
  const worst = worstSeverity(snapshots, credentialProblem);

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
        <Notice kind="auth">
          Login problem: overseer's own credential is failing ({credential.consecutiveFailures} time
          {credential.consecutiveFailures === 1 ? "" : "s"} in a row), not a go-api outage.
        </Notice>
      ) : null}
    </div>
  );
}
