import type { CSSProperties } from "react";
import type { PanelProps, State } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Panel file convention (see registry.tsx / liveactivity.panel.tsx): a bucket
// panel lives at web/src/panels/<bucketId>.panel.tsx, default-exports a
// React.FC<PanelProps<D>>, and co-locates its own payload type D here. No edit to
// registry.tsx / types.ts — the registry globs this directory and keys it "cost".

// The payload types below mirror the exact JSON the cost bucket emits
// (internal/buckets/cost/cost.go -> Data). The two halves — OCI infra spend and
// go-api provider usage — degrade INDEPENDENTLY, each with its own stale flag and
// bounded trend. Watched-app strings (service names, provider names, trend text)
// are rendered as plain text; React escapes them, never as HTML.

// SpendLine mirrors oci.SpendLine's camelCase json tags (see oci.go).
interface SpendLine {
  service: string;
  amount: number;
}

// Spend mirrors oci.Spend's camelCase json tags: the month-to-date infra total,
// its currency, the window it covers and the per-service breakdown (largest
// first). Service names are not identifiers.
interface Spend {
  amount: number;
  currency: string;
  periodStart: string;
  periodEnd: string;
  lines: SpendLine[] | null;
}

// ProviderOutcomes mirrors goapi.ProviderOutcomes (lowercase json tags): one
// provider's calls split by outcome.
interface ProviderOutcomes {
  ok: number;
  quota: number;
  error: number;
}

// ProviderUsage mirrors goapi.ProviderUsage: provider name -> outcome counts.
type ProviderUsage = Record<string, ProviderOutcomes>;

// Data is the cost panel payload: the OCI infra-spend half and the go-api
// provider-usage half, each with a last-known value and an independent stale flag.
// This mirrors cost.Data field-for-field; the per-source bounded trends
// (spendTrend, usageTrend) it once carried were dropped from the Go payload in
// #1484, so nothing extra comes over the wire. Any future field parses harmlessly.
export interface Data {
  spend: Spend | null;
  spendStale: boolean;
  usage: ProviderUsage | null;
  usageStale: boolean;
}

// halfState mirrors the Go costState logic per source: fresh is live, stale with a
// last-known value is stale, stale with nothing ever read is source_down. Each half
// shows its own badge so a single source down reads clearly against a live sibling.
function halfState(stale: boolean, hasData: boolean): State {
  if (!stale) return "live";
  return hasData ? "stale" : "source_down";
}

// formatMoney renders an amount in its currency. Intl currency formatting needs a
// valid ISO code; a missing/odd currency (dev, unconfigured OCI) falls back to a
// plain fixed-2 with the raw code appended, never throwing.
function formatMoney(amount: number, currency: string): string {
  const code = currency.trim();
  if (code) {
    try {
      return new Intl.NumberFormat(undefined, {
        style: "currency",
        currency: code,
        maximumFractionDigits: 2,
      }).format(amount);
    } catch {
      // fall through to the plain rendering below
    }
  }
  const plain = amount.toFixed(2);
  return code ? `${plain} ${code}` : plain;
}

const sumOutcomes = (o: ProviderOutcomes): number => o.ok + o.quota + o.error;

// CostPanel is the bespoke panel for the cost bucket: at a glance, what Altune is
// spending. Headline totals, an OCI infra-spend breakdown by service, and a
// provider-API usage breakdown by outcome. The two halves degrade independently —
// each keeps its last-known value (dimmed when its own source is down) with a
// per-half badge, so a single source down never blanks the panel or the sibling.
export default function CostPanel({ snapshot }: Pick<PanelProps<Data>, "snapshot">) {
  const data = snapshot.data;
  const spend = data.spend;
  const usage = data.usage ?? {};
  const spendLines = spend?.lines ?? [];
  const providers = Object.entries(usage);

  const spendState = halfState(data.spendStale, spend != null);
  const usageState = halfState(data.usageStale, data.usage != null);

  const callTotal = providers.reduce((n, [, o]) => n + sumOutcomes(o), 0);
  const spendMax = spendLines.reduce((n, l) => Math.max(n, l.amount), 0);
  const callMax = providers.reduce((n, [, o]) => Math.max(n, sumOutcomes(o)), 0);

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      <div className="la-metrics">
        <Metric
          value={spend ? formatMoney(spend.amount, spend.currency) : "—"}
          label="infra spend"
        />
        <Metric value={callTotal} label="provider calls" />
        <Metric value={providers.length} label="providers" />
      </div>

      {snapshot.state === "source_down" && (
        <p className="notice">both sources unreachable — showing last-known cost.</p>
      )}

      <div style={sec.grid}>
        {/* OCI infra-spend half */}
        <section style={sec.block}>
          <SectionHead title="Infra spend (OCI)" state={spendState} />
          {spend ? (
            <div style={sec.body(spendState !== "live")}>
              <p style={sec.sub}>
                {formatMoney(spend.amount, spend.currency)} month-to-date
                {spend.periodStart ? ` · since ${formatUpdated(spend.periodStart)}` : ""}
              </p>
              {spendLines.length === 0 ? (
                <p className="empty" style={sec.emptyRow}>
                  no per-service breakdown
                </p>
              ) : (
                <ul style={sec.list}>
                  {spendLines.map((l, i) => (
                    <li key={`${l.service}-${i}`} style={sec.row}>
                      <div style={sec.fill(pct(l.amount, spendMax))} />
                      <span style={sec.label}>{l.service}</span>
                      <span style={sec.count}>{formatMoney(l.amount, spend.currency)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ) : (
            <p className="empty" style={sec.emptyRow}>
              no spend read yet
            </p>
          )}
        </section>

        {/* go-api provider-usage half */}
        <section style={sec.block}>
          <SectionHead title="Provider API usage" state={usageState} />
          {providers.length === 0 ? (
            <p className="empty" style={sec.emptyRow}>
              no provider usage yet
            </p>
          ) : (
            <div style={sec.body(usageState !== "live")}>
              <ul style={sec.list}>
                {providers.map(([name, o]) => (
                  <ProviderRow key={name} name={name} outcomes={o} max={callMax} />
                ))}
              </ul>
              <Legend />
            </div>
          )}
        </section>
      </div>

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

const pct = (v: number, max: number): number =>
  max <= 0 ? 0 : Math.round((v / max) * 100);

function Metric({ value, label }: { value: number | string; label: string }) {
  return (
    <div className="metric">
      <span className="metric-value">{value}</span>
      <span className="metric-label">{label}</span>
    </div>
  );
}

// SectionHead labels a half and shows that half's own state, so an independently
// stale source reads clearly next to a live sibling.
function SectionHead({ title, state }: { title: string; state: State }) {
  return (
    <div style={sec.head}>
      <span style={sec.headTitle}>{title}</span>
      <StateBadge state={state} />
    </div>
  );
}

// ProviderRow renders one provider's calls as a Grafana-style segmented bar —
// ok/quota/error proportions in the theme's live/stale/down colors — with the
// total to the right. The provider name is watched-app text (React-escaped).
function ProviderRow({
  name,
  outcomes,
  max,
}: {
  name: string;
  outcomes: ProviderOutcomes;
  max: number;
}) {
  const total = sumOutcomes(outcomes);
  const width = pct(total, max);
  return (
    <li style={sec.provRow}>
      <div style={sec.provHead}>
        <span style={sec.label}>{name}</span>
        <span style={sec.count}>{total}</span>
      </div>
      <div
        style={sec.track}
        role="img"
        aria-label={`${name}: ${outcomes.ok} ok, ${outcomes.quota} quota, ${outcomes.error} error`}
      >
        <div style={sec.segTrack(width)}>
          <div style={sec.seg(outcomes.ok, total, "var(--live)")} />
          <div style={sec.seg(outcomes.quota, total, "var(--stale)")} />
          <div style={sec.seg(outcomes.error, total, "var(--down)")} />
        </div>
      </div>
    </li>
  );
}

function Legend() {
  return (
    <div style={sec.legend}>
      <LegendDot color="var(--live)" label="ok" />
      <LegendDot color="var(--stale)" label="quota" />
      <LegendDot color="var(--down)" label="error" />
    </div>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span style={sec.legendItem}>
      <span style={{ ...sec.legendDot, background: color }} />
      {label}
    </span>
  );
}

// On-theme inline styles (via CSS variables from styles.css). Kept in the panel
// file so a new bucket panel stays a single-file, additive change.
const sec = {
  grid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))",
    gap: 16,
  } as CSSProperties,
  block: { display: "flex", flexDirection: "column", gap: 8 } as CSSProperties,
  body: (dim: boolean): CSSProperties => ({
    display: "flex",
    flexDirection: "column",
    gap: 8,
    opacity: dim ? 0.55 : 1,
  }),
  head: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  } as CSSProperties,
  headTitle: {
    fontSize: 11,
    textTransform: "uppercase",
    letterSpacing: "0.06em",
    color: "var(--fg-dim)",
  } as CSSProperties,
  sub: {
    margin: 0,
    fontSize: 12,
    fontFamily: "var(--mono)",
    color: "var(--fg-faint)",
  } as CSSProperties,
  list: {
    listStyle: "none",
    margin: 0,
    padding: 0,
    display: "flex",
    flexDirection: "column",
    gap: 4,
  } as CSSProperties,
  row: {
    position: "relative",
    display: "grid",
    gridTemplateColumns: "1fr auto",
    alignItems: "center",
    gap: 10,
    padding: "5px 8px",
    borderRadius: 6,
    overflow: "hidden",
    background: "var(--bg-elev-2)",
    fontSize: 13,
  } as CSSProperties,
  fill: (p: number): CSSProperties => ({
    position: "absolute",
    inset: 0,
    width: `${p}%`,
    background: "rgba(91, 140, 255, 0.14)",
    borderRight: "1px solid rgba(91, 140, 255, 0.4)",
  }),
  label: {
    position: "relative",
    color: "var(--fg)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  } as CSSProperties,
  count: {
    position: "relative",
    fontFamily: "var(--mono)",
    fontSize: 12,
    color: "var(--fg-dim)",
  } as CSSProperties,
  provRow: {
    display: "flex",
    flexDirection: "column",
    gap: 4,
    padding: "6px 8px",
    borderRadius: 6,
    background: "var(--bg-elev-2)",
  } as CSSProperties,
  provHead: {
    display: "flex",
    alignItems: "baseline",
    justifyContent: "space-between",
    gap: 10,
    fontSize: 13,
  } as CSSProperties,
  track: {
    height: 8,
    borderRadius: 4,
    background: "var(--bg-elev)",
    overflow: "hidden",
  } as CSSProperties,
  segTrack: (width: number): CSSProperties => ({
    display: "flex",
    height: "100%",
    width: `${width}%`,
  }),
  seg: (v: number, total: number, color: string): CSSProperties => ({
    width: total <= 0 ? "0%" : `${(v / total) * 100}%`,
    background: color,
  }),
  legend: {
    display: "flex",
    gap: 12,
    fontSize: 10,
    color: "var(--fg-faint)",
  } as CSSProperties,
  legendItem: {
    display: "inline-flex",
    alignItems: "center",
    gap: 5,
  } as CSSProperties,
  legendDot: {
    width: 8,
    height: 8,
    borderRadius: 2,
    display: "inline-block",
  } as CSSProperties,
  emptyRow: { margin: 0, fontSize: 13 } as CSSProperties,
};
