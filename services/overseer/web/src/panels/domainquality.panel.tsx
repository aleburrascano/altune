import type { CSSProperties } from "react";
import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Bespoke panel for the `domainquality` bucket. It follows the file convention
// (see registry.tsx / liveactivity.panel.tsx): a default-exported
// React.FC<PanelProps<D>> with its payload type D co-located here. The registry
// auto-discovers it by filename, so nothing else is edited.
//
// The payload mirrors the Go bucket's Snapshot().Data
// (internal/buckets/domainquality/domainquality.go). Top-level keys are camelCase
// (the bucket's own json tags); the nested go-api mirrors keep their snake_case
// tags. Artist / query strings are watched-app data rendered as plain text —
// React escapes them, never dangerouslySetInnerHTML.

export interface EvalQuery {
  query: string;
  expect: string;
  passed: boolean;
  position: number;
}

export interface EvalStatus {
  enabled: boolean;
  paused: boolean;
  state: string;
  score?: number | null;
  baseline?: number | null;
  last_run?: string | null;
  error?: string;
  queries?: EvalQuery[];
}

export interface AcquisitionStatus {
  in_flight: number;
  succeeded: number;
  failed: number;
  rejected: number;
  queue_depth: number;
  queue_capacity: number;
}

export interface DiscographyCase {
  artist: string;
  artist_ref: string;
  releases: number;
  single_provider: number;
  single_provider_no_id: number;
  provider_counts: Record<string, number> | null;
  last_seen: string;
}

export interface DiscographyQuality {
  window_days: number;
  group_by: string;
  cases: DiscographyCase[] | null;
}

export interface Signal {
  at: string;
  kind: string;
  text: string;
}

// Data is the domain-quality panel payload — the two anchor reads (eval meter,
// acquisition health) plus the worst-first discography aggregate, each with its
// own independent stale flag so a half-live panel reads honestly, and the bounded
// discography trend (rendered as the latest sample). This mirrors
// domainquality.Data field-for-field; the discoPivots (alternate group-bys) and
// cross-source history ring it once carried were dropped from the Go payload in
// #1484, so nothing extra comes over the wire. Any future field parses harmlessly.
export interface Data {
  eval: EvalStatus | null;
  evalStale: boolean;
  acquisition: AcquisitionStatus | null;
  acqStale: boolean;
  discography: DiscographyQuality | null;
  discoStale: boolean;
  discoTrend: Signal[] | null;
}

// noIDSuspectRatio is the id-anchored contamination signal: the fraction of an
// artist's releases that exactly one provider supplied AND that carry no shared
// id. The id is the anchor, so an id-verified single-provider release is not a
// suspect and never scores high. It mirrors go-api's noIDSuspectRatio (the
// worst-first primary key). Pure arithmetic over the served counts; a caseless /
// zero-release case is not rateable.
function noIDSuspectRatio(c: DiscographyCase): number | null {
  if (c.releases <= 0) return null;
  return c.single_provider_no_id / c.releases;
}

// rateableCases keeps the go-api worst-first order intact — the verdict is
// computed in go-api and the panel renders the served aggregate, never re-ranks —
// dropping only the zero-release cases the ratio cannot grade.
function rateableCases(cases: DiscographyCase[]): DiscographyCase[] {
  return cases.filter((c) => noIDSuspectRatio(c) !== null);
}

function pct(fraction: number): string {
  return `${Math.round(fraction * 100)}%`;
}

// acquisitionRate mirrors the Go SuccessRate: succeeded / (succeeded + failed),
// undefined when no job has completed (never a spurious 0% or divide-by-zero).
function acquisitionRate(a: AcquisitionStatus): number | null {
  const completed = a.succeeded + a.failed;
  if (completed <= 0) return null;
  return a.succeeded / completed;
}

// caseLabel prefers the human artist name, falling back to the stable ref.
function caseLabel(c: DiscographyCase): string {
  return c.artist || c.artist_ref || "unknown";
}

const sectionStyle: CSSProperties = {
  borderTop: "1px solid var(--border)",
  paddingTop: 12,
  display: "flex",
  flexDirection: "column",
  gap: 10,
};

const subHeadStyle: CSSProperties = {
  display: "flex",
  alignItems: "baseline",
  justifyContent: "space-between",
  gap: 8,
};

const subTitleStyle: CSSProperties = {
  fontSize: 11,
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  color: "var(--fg-dim)",
  fontFamily: "var(--mono)",
};

const staleTagStyle: CSSProperties = {
  fontFamily: "var(--mono)",
  fontSize: 10,
  letterSpacing: "0.06em",
  color: "var(--stale)",
};

function StaleTag({ show }: { show: boolean }) {
  if (!show) return null;
  return <span style={staleTagStyle}>STALE</span>;
}

// severityColor grades a contamination ratio from healthy (green) through
// caution (amber) to alarming (red) — the Grafana traffic-light read.
function severityColor(ratio: number): string {
  if (ratio >= 0.5) return "var(--down)";
  if (ratio >= 0.2) return "var(--stale)";
  return "var(--live)";
}

export default function DomainQualityPanel({ snapshot }: PanelProps<Data>) {
  const data = snapshot.data;
  const evalMeter = data.eval;
  const acq = data.acquisition;
  const disco = data.discography;
  const cases = rateableCases(disco?.cases ?? []);
  const trend = data.discoTrend ?? [];
  const latestTrend = trend.length > 0 ? trend[trend.length - 1] : null;
  const dimmed = snapshot.state === "source_down";

  const scored = evalMeter != null && evalMeter.score != null;
  const baseline = evalMeter?.baseline ?? null;
  const scoreDelta =
    scored && baseline != null ? (evalMeter?.score ?? 0) - baseline : null;

  const acqRate = acq != null ? acquisitionRate(acq) : null;

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      {snapshot.state === "source_down" && (
        <p className="notice" style={{ color: "var(--down)" }}>
          go-api unreachable — showing last-known domain quality.
        </p>
      )}
      {snapshot.state === "stale" && (
        <p className="notice">a source read is stale — showing last-known values.</p>
      )}

      {/* Anchor reads: the two hero signals that drive the panel state. */}
      <div className="la-metrics" style={{ opacity: dimmed ? 0.6 : 1 }}>
        <div className="metric">
          <span className="metric-value" style={{ color: scored ? "var(--fg)" : "var(--fg-faint)" }}>
            {scored ? (evalMeter?.score ?? 0).toFixed(2) : "—"}
          </span>
          <span className="metric-label">
            eval score <StaleTag show={data.evalStale} />
          </span>
          <span style={{ fontSize: 11, color: "var(--fg-faint)", fontFamily: "var(--mono)" }}>
            {baseline != null ? `baseline ${baseline.toFixed(2)}` : "no baseline"}
            {scoreDelta != null && (
              <span style={{ color: scoreDelta >= 0 ? "var(--live)" : "var(--down)" }}>
                {" "}
                {scoreDelta >= 0 ? "+" : ""}
                {scoreDelta.toFixed(2)}
              </span>
            )}
          </span>
        </div>

        <div className="metric">
          <span
            className="metric-value"
            style={{ color: acqRate != null ? severityColorForRate(acqRate) : "var(--fg-faint)" }}
          >
            {acqRate != null ? pct(acqRate) : "—"}
          </span>
          <span className="metric-label">
            acquisition <StaleTag show={data.acqStale} />
          </span>
          <span style={{ fontSize: 11, color: "var(--fg-faint)", fontFamily: "var(--mono)" }}>
            {acq != null ? `ok ${acq.succeeded} · fail ${acq.failed} · q ${acq.queue_depth}/${acq.queue_capacity}` : "no data"}
          </span>
        </div>
      </div>

      {evalMeter?.state && (
        <div style={{ fontSize: 12, color: "var(--fg-dim)" }}>
          meter <span style={{ fontFamily: "var(--mono)", color: "var(--fg)" }}>{evalMeter.state}</span>
          {evalMeter.paused && <span style={{ color: "var(--stale)" }}> · paused</span>}
          {!evalMeter.enabled && <span style={{ color: "var(--fg-faint)" }}> · disabled</span>}
          {evalMeter.error && <span style={{ color: "var(--down)" }}> · {evalMeter.error}</span>}
        </div>
      )}

      {/* Discography structural quality: worst-first contamination read. */}
      <section style={sectionStyle}>
        <div style={subHeadStyle}>
          <span style={subTitleStyle}>Discography · worst first</span>
          <span style={{ fontSize: 11, color: "var(--fg-faint)", fontFamily: "var(--mono)" }}>
            {disco != null ? `by ${disco.group_by} · ${disco.window_days}d ` : ""}
            <StaleTag show={data.discoStale} />
          </span>
        </div>

        {cases.length === 0 ? (
          <p className="empty">no rateable discographies</p>
        ) : (
          <ul
            style={{
              listStyle: "none",
              margin: 0,
              padding: 0,
              display: "flex",
              flexDirection: "column",
              gap: 8,
              maxHeight: 260,
              overflowY: "auto",
              opacity: dimmed ? 0.6 : 1,
            }}
          >
            {cases.slice(0, 8).map((c, i) => {
              const ratio = noIDSuspectRatio(c) ?? 0;
              return (
                <li key={`${c.artist_ref}-${i}`} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                  <div style={{ display: "flex", justifyContent: "space-between", gap: 10, alignItems: "baseline" }}>
                    <span
                      style={{
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        color: "var(--fg)",
                      }}
                    >
                      {caseLabel(c)}
                    </span>
                    <span
                      style={{
                        fontFamily: "var(--mono)",
                        fontSize: 12,
                        color: severityColor(ratio),
                        flex: "none",
                      }}
                    >
                      {pct(ratio)}
                    </span>
                  </div>
                  <div
                    style={{
                      height: 4,
                      borderRadius: 999,
                      background: "var(--bg)",
                      border: "1px solid var(--border)",
                      overflow: "hidden",
                    }}
                    role="presentation"
                  >
                    <div
                      style={{
                        width: `${Math.min(100, Math.round(ratio * 100))}%`,
                        height: "100%",
                        background: severityColor(ratio),
                      }}
                    />
                  </div>
                  <span style={{ fontSize: 11, color: "var(--fg-faint)", fontFamily: "var(--mono)" }}>
                    {c.single_provider}/{c.releases} single-provider, {c.single_provider_no_id} without a shared id
                  </span>
                </li>
              );
            })}
          </ul>
        )}

        {latestTrend && (
          <p style={{ margin: 0, fontSize: 11, color: "var(--fg-faint)", fontFamily: "var(--mono)" }}>
            trend: {latestTrend.text}
          </p>
        )}
      </section>

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

// severityColorForRate grades an acquisition SUCCESS rate (inverse of the
// contamination read: higher is healthier).
function severityColorForRate(rate: number): string {
  if (rate >= 0.9) return "var(--live)";
  if (rate >= 0.7) return "var(--stale)";
  return "var(--down)";
}
