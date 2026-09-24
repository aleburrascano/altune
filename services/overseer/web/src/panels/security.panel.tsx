import type { CSSProperties } from "react";
import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Panel file convention (see registry.tsx / liveactivity.panel.tsx): a bucket
// panel lives at web/src/panels/<bucketId>.panel.tsx, default-exports a
// React.FC<PanelProps<D>>, and co-locates its own payload type D here. No edit to
// registry.tsx / types.ts / styles.css — the registry globs this directory and
// keys this module "security".

// CheckView mirrors the Go security.CheckView (internal/buckets/security/view.go):
// one self-test's outcome. `reached=false` means the probe could not run (transport
// failure or the fence refusing an off-allowlist target) — neither pass nor fail,
// which is what degrades the bucket to stale. `error` is reflected transport text:
// watched-app data rendered as plain text (React escapes it), never as HTML.
interface CheckView {
  name: string;
  desc: string;
  reached: boolean;
  passed: boolean;
  status: number;
  error?: string;
}

// HistoryEntry mirrors the Go core.Signal folded per run: a timestamped one-line
// verdict summary. `text` is watched-app-adjacent text rendered plainly.
interface HistoryEntry {
  at: string;
  kind: string;
  text: string;
}

// Data mirrors the Go security.Data payload: the overall self-test verdict
// (passed / total), the per-check outcomes, and the bounded run history. The
// security bucket fires a fenced, read-only self-test suite at go-api's own
// surface and reports the pass/fail verdict — a live proof the hardening holds.
export interface Data {
  hasRun: boolean;
  passed: number;
  total: number;
  lastRun: string;
  checks: CheckView[];
  history: HistoryEntry[];
}

type Verdict = "pass" | "fail" | "unreached";

// verdict classifies one check for display: a check that never reached go-api is
// "unreached" (amber, drives stale) regardless of its passed flag; otherwise pass
// (green) or fail (red).
function verdict(c: CheckView): Verdict {
  if (!c.reached) return "unreached";
  return c.passed ? "pass" : "fail";
}

const GLYPH: Record<Verdict, string> = { pass: "✓", fail: "✕", unreached: "–" };
const TONE: Record<Verdict, string> = {
  pass: "var(--live)",
  fail: "var(--down)",
  unreached: "var(--stale)",
};

// SecurityPanel is the bespoke panel for the security bucket: a headline verdict
// gauge (checks passed of total), the per-check self-test grid with pass/fail/
// unreached tones and each check's rejection status, and a compact run-history
// strip. All three states render cleanly — live shows the fresh verdict; stale and
// source_down keep showing the last-known verdict (dimmed on source_down) with a
// notice, so the panel never goes blank.
export default function SecurityPanel({ snapshot }: Pick<PanelProps<Data>, "snapshot">) {
  const data = snapshot.data;
  const checks = data.checks ?? [];
  const history = data.history ?? [];
  const down = snapshot.state === "source_down";
  const stale = snapshot.state === "stale";
  const total = data.total || checks.length;
  const passed = data.passed ?? 0;
  const failing = checks.filter((c) => verdict(c) === "fail").length;
  const unreached = checks.filter((c) => verdict(c) === "unreached").length;
  const allPass = total > 0 && passed === total && failing === 0 && unreached === 0;

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      {!data.hasRun ? (
        <p className="empty">no self-test run yet</p>
      ) : (
        <div style={sec.body(down)}>
          <div style={sec.verdict}>
            <div style={sec.gauge(allPass, failing > 0)}>
              <span style={sec.gaugeNum}>{passed}</span>
              <span style={sec.gaugeDen}>/ {total}</span>
            </div>
            <div style={sec.verdictText}>
              <span style={sec.verdictHead(allPass, failing > 0)}>
                {allPass ? "all defenses held" : failing > 0 ? "regression detected" : "partial run"}
              </span>
              <span style={sec.verdictSub}>
                {failing > 0 ? `${failing} failing` : null}
                {failing > 0 && unreached > 0 ? " · " : null}
                {unreached > 0 ? `${unreached} unreached` : null}
                {failing === 0 && unreached === 0 ? "self-tests passing" : null}
              </span>
            </div>
          </div>

          {down && (
            <p className="notice">go-api unreachable — showing last-known verdict.</p>
          )}
          {stale && (
            <p className="notice">self-test stalled — showing last-known verdict.</p>
          )}

          <section style={sec.block}>
            <SectionHead title="Self-tests" trailing={checks.length ? `${checks.length}` : ""} />
            {checks.length === 0 ? (
              <p className="empty" style={sec.emptyRow}>
                no checks recorded
              </p>
            ) : (
              <ul style={sec.list}>
                {checks.map((c, i) => (
                  <CheckRow key={`${c.name}-${i}`} check={c} />
                ))}
              </ul>
            )}
          </section>

          {history.length > 0 && (
            <section style={sec.block}>
              <SectionHead title="History" trailing={`${history.length} runs`} />
              <ul style={sec.histList}>
                {[...history].reverse().map((h, i) => (
                  <li key={`${h.at}-${i}`} style={sec.histRow}>
                    <span style={sec.histText}>{h.text}</span>
                    <time style={sec.histTime}>{formatUpdated(h.at)}</time>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

// CheckRow renders one self-test: a verdict glyph in the check's tone, its name and
// human description, and (when reached) the rejection status the app answered with,
// or (when unreached / failing) the reflected error. Name, desc and error are all
// rendered as plain text — React escapes them, so hostile reflected strings can
// never inject markup.
function CheckRow({ check }: { check: CheckView }) {
  const v = verdict(check);
  return (
    <li style={sec.row}>
      <span style={sec.glyph(TONE[v])} aria-hidden>
        {GLYPH[v]}
      </span>
      <div style={sec.rowMain}>
        <span style={sec.rowName}>{check.name}</span>
        <span style={sec.rowDesc}>{check.desc}</span>
        {check.error ? <span style={sec.rowErr}>{check.error}</span> : null}
      </div>
      <span style={sec.rowStatus(TONE[v])}>
        {check.reached ? (check.status || "—") : "unreached"}
      </span>
    </li>
  );
}

function SectionHead({ title, trailing }: { title: string; trailing?: string }) {
  return (
    <div style={sec.head}>
      <span style={sec.headTitle}>{title}</span>
      {trailing ? <span style={sec.headTrail}>{trailing}</span> : null}
    </div>
  );
}

// On-theme inline styles (via CSS variables from styles.css). Kept in the panel
// file so a new bucket panel stays a single-file, additive change.
const sec = {
  body: (dim: boolean): CSSProperties => ({
    display: "flex",
    flexDirection: "column",
    gap: 16,
    opacity: dim ? 0.55 : 1,
  }),
  verdict: {
    display: "flex",
    alignItems: "center",
    gap: 16,
  } as CSSProperties,
  gauge: (pass: boolean, fail: boolean): CSSProperties => ({
    display: "flex",
    alignItems: "baseline",
    gap: 4,
    padding: "8px 14px",
    borderRadius: 10,
    background: "var(--bg-elev-2)",
    border: `1px solid ${fail ? "var(--down)" : pass ? "var(--live)" : "var(--border-strong)"}`,
  }),
  gaugeNum: {
    fontSize: 28,
    fontWeight: 600,
    fontFamily: "var(--mono)",
    color: "var(--fg)",
    lineHeight: 1,
  } as CSSProperties,
  gaugeDen: {
    fontSize: 14,
    fontFamily: "var(--mono)",
    color: "var(--fg-faint)",
  } as CSSProperties,
  verdictText: { display: "flex", flexDirection: "column", gap: 2 } as CSSProperties,
  verdictHead: (pass: boolean, fail: boolean): CSSProperties => ({
    fontSize: 14,
    fontWeight: 600,
    color: fail ? "var(--down)" : pass ? "var(--live)" : "var(--stale)",
  }),
  verdictSub: {
    fontSize: 12,
    color: "var(--fg-dim)",
  } as CSSProperties,
  block: { display: "flex", flexDirection: "column", gap: 8 } as CSSProperties,
  head: {
    display: "flex",
    alignItems: "baseline",
    justifyContent: "space-between",
    gap: 12,
  } as CSSProperties,
  headTitle: {
    fontSize: 11,
    textTransform: "uppercase",
    letterSpacing: "0.06em",
    color: "var(--fg-dim)",
  } as CSSProperties,
  headTrail: {
    fontSize: 11,
    fontFamily: "var(--mono)",
    color: "var(--fg-faint)",
  } as CSSProperties,
  list: { listStyle: "none", margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 4 } as CSSProperties,
  row: {
    display: "grid",
    gridTemplateColumns: "auto 1fr auto",
    alignItems: "start",
    gap: 10,
    padding: "8px 10px",
    borderRadius: 6,
    background: "var(--bg-elev-2)",
  } as CSSProperties,
  glyph: (tone: string): CSSProperties => ({
    fontFamily: "var(--mono)",
    fontSize: 14,
    fontWeight: 700,
    color: tone,
    lineHeight: "18px",
  }),
  rowMain: { display: "flex", flexDirection: "column", gap: 2, minWidth: 0 } as CSSProperties,
  rowName: {
    fontSize: 13,
    fontFamily: "var(--mono)",
    color: "var(--fg)",
  } as CSSProperties,
  rowDesc: {
    fontSize: 12,
    color: "var(--fg-dim)",
  } as CSSProperties,
  rowErr: {
    fontSize: 11,
    fontFamily: "var(--mono)",
    color: "var(--down)",
    wordBreak: "break-word",
  } as CSSProperties,
  rowStatus: (tone: string): CSSProperties => ({
    fontFamily: "var(--mono)",
    fontSize: 12,
    color: tone,
    whiteSpace: "nowrap",
  }),
  histList: { listStyle: "none", margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 2 } as CSSProperties,
  histRow: {
    display: "flex",
    alignItems: "baseline",
    justifyContent: "space-between",
    gap: 10,
    padding: "3px 2px",
    borderBottom: "1px solid var(--border)",
    fontSize: 12,
  } as CSSProperties,
  histText: {
    color: "var(--fg-dim)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  } as CSSProperties,
  histTime: {
    fontFamily: "var(--mono)",
    fontSize: 10,
    color: "var(--fg-faint)",
    whiteSpace: "nowrap",
  } as CSSProperties,
  emptyRow: { margin: 0, fontSize: 13 } as CSSProperties,
};
