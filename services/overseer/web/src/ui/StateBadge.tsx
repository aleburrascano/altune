import type { Reason, Severity, State } from "../types";

const STATE_LABELS: Record<State, string> = {
  live: "LIVE",
  stale: "STALE",
  source_down: "SOURCE DOWN",
};

export const REASON_LABELS: Record<Reason, string> = {
  auth: "login problem",
  throttled: "throttled",
  degraded: "degraded",
  down: "unreachable",
  connecting: "reconnecting",
};

const SEVERITY_TONES: Record<Severity, string> = {
  ok: "text-ok bg-ok/10 border-ok/30",
  warn: "text-warn bg-warn/10 border-warn/30",
  critical: "text-critical bg-critical/10 border-critical/30",
};

const SEVERITY_DOTS: Record<Severity, string> = {
  ok: "bg-ok",
  warn: "bg-warn",
  critical: "bg-critical",
};

export function StateBadge({ state, severity, reason }: { state: State; severity: Severity; reason?: Reason }) {
  return (
    <span
      data-severity={severity}
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 font-mono text-2xs tracking-wider ${SEVERITY_TONES[severity]}`}
    >
      <span aria-hidden="true" className={`size-1.5 rounded-full ${SEVERITY_DOTS[severity]}`} />
      <span>{STATE_LABELS[state]}</span>
      {reason ? <span className="opacity-80">{REASON_LABELS[reason]}</span> : null}
      <span className="sr-only">severity {severity}</span>
    </span>
  );
}
