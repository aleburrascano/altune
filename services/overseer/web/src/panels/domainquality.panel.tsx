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
  // suspect_rate is go-api's windowed headline in [0,1]: the share of real
  // discography opens whose top release-suspect fired. last_sample_at is the most
  // recent real open's time, shown beside the headline as its freshness. Both are
  // served by go-api (computed over real opens only) — the panel renders them, it
  // never recomputes. Optional so an older go-api payload decodes cleanly.
  suspect_rate?: number;
  last_sample_at?: string;
}

export interface Signal {
  at: string;
  kind: string;
  text: string;
}

// AcqWindow is the recent-window acquisition success rate the panel renders as the
// acquisition headline instead of the lifetime ratio: a spike of current failures
// shows here even while the all-time average stays high. It is null when the window
// holds no completed jobs (warming up, idle, or just after a counter reset).
export interface AcqWindow {
  rate: number;
  completed: number;
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
  // evalAgeStale flags a score go-api last computed longer ago than its freshness
  // threshold — independent of evalStale, which is read reachability.
  evalAgeStale: boolean;
  acquisition: AcquisitionStatus | null;
  acqStale: boolean;
  // acqWindow is the recent-window success rate; null when no job completed in the
  // window, so the headline shows "—" rather than a spurious 0%.
  acqWindow: AcqWindow | null;
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

// sampleAge renders how long ago the headline's last real sample landed, so a
// stale rate can never read as live (freshness shown, never faked). A missing or
// zero-time value — no open in the window — reads "no samples", never "0s ago".
function sampleAge(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "no samples";
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "no samples";
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 60) return `sample ${secs}s ago`;
  if (secs < 3600) return `sample ${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `sample ${Math.floor(secs / 3600)}h ago`;
  return `sample ${Math.floor(secs / 86400)}d ago`;
}

// evalAge renders how long ago go-api last scored the eval meter, so a score that
// is fresh to fetch but stale to compute cannot read as live (freshness shown,
// never faked). A missing or zero-time last_run — the meter never ran — reads
// "never run", never "0s ago".
function evalAge(iso: string | undefined | null, now: number = Date.now()): string {
  if (!iso) return "never run";
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "never run";
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 60) return `ran ${secs}s ago`;
  if (secs < 3600) return `ran ${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `ran ${Math.floor(secs / 3600)}h ago`;
  return `ran ${Math.floor(secs / 86400)}d ago`;
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

export default function DomainQualityPanel({ snapshot }: Pick<PanelProps<Data>, "snapshot">) {
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

  const acqWindow = data.acqWindow;
  const acqRate = acqWindow?.rate ?? null;
  const evalAgeStale = data.evalAgeStale;
  const suspectRate = disco?.suspect_rate ?? null;

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
          <span style={{ fontSize: 11, color: evalAgeStale ? "var(--stale)" : "var(--fg-faint)", fontFamily: "var(--mono)" }}>
            {evalAge(evalMeter?.last_run)}
            {evalAgeStale && " · STALE"}
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
            {acq == null
              ? "no data"
              : acqWindow != null
                ? `${acqWindow.completed} recent · q ${acq.queue_depth}/${acq.queue_capacity}`
                : `no recent completions · q ${acq.queue_depth}/${acq.queue_capacity}`}
          </span>
        </div>

        {/* Suspect rate: the "is the product suspect right now" headline — the share
            of real discography opens whose top suspect fired. go-api computes it over
            real opens only; the panel renders the served number and its freshness. */}
        <div className="metric">
          <span
            className="metric-value"
            style={{ color: suspectRate != null ? severityColor(suspectRate) : "var(--fg-faint)" }}
          >
            {suspectRate != null ? pct(suspectRate) : "—"}
          </span>
          <span className="metric-label">
            suspect rate <StaleTag show={data.discoStale} />
          </span>
          <span style={{ fontSize: 11, color: "var(--fg-faint)", fontFamily: "var(--mono)" }}>
            {sampleAge(disco?.last_sample_at)}
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
