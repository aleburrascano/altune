import type { PanelProps } from "../types";
import { Panel } from "../ui";

const MAX_DEPTH = 3;
const MAX_ARRAY_PREVIEW = 3;

export function GenericPanel({ snapshot }: Pick<PanelProps, "snapshot">) {
  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <KeyValueView value={snapshot.data} depth={0} />
    </Panel>
  );
}

export function formatUpdated(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "never";
  return new Date(t).toLocaleString();
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function KeyValueView({ value, depth }: { value: unknown; depth: number }) {
  if (value === null || value === undefined) {
    return <ScalarValue text="—" />;
  }
  if (Array.isArray(value)) {
    return <ArrayValue items={value} depth={depth} />;
  }
  if (isPlainObject(value)) {
    return <ObjectValue fields={value} depth={depth} />;
  }
  return <ScalarValue text={String(value)} />;
}

function ScalarValue({ text }: { text: string }) {
  return <span className="min-w-0 truncate font-mono text-sm text-fg">{text}</span>;
}

function ObjectValue({ fields, depth }: { fields: Record<string, unknown>; depth: number }) {
  const keys = Object.keys(fields);
  if (keys.length === 0) return <ScalarValue text="empty object" />;
  if (depth >= MAX_DEPTH) return <ScalarValue text={`${keys.length} field${keys.length === 1 ? "" : "s"}`} />;
  return (
    <dl className="m-0 flex min-w-0 flex-col gap-1 border-l border-border pl-3">
      {keys.map((key) => (
        <div key={key} className="flex min-w-0 flex-wrap items-baseline gap-2">
          <dt className="font-mono text-xs uppercase tracking-wider text-fg-dim">{key}</dt>
          <dd className="m-0 min-w-0">
            <KeyValueView value={fields[key]} depth={depth + 1} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

function summarizedArray(items: unknown[]): string {
  const preview = items.slice(0, MAX_ARRAY_PREVIEW).map((item) => String(item)).join(", ");
  const rest = items.length - MAX_ARRAY_PREVIEW;
  return rest > 0 ? `${preview}, …+${rest} more (${items.length})` : `${preview} (${items.length})`;
}

function ArrayValue({ items, depth }: { items: unknown[]; depth: number }) {
  if (items.length === 0) return <ScalarValue text="empty list" />;
  const isScalarArray = items.every((item) => !isPlainObject(item) && !Array.isArray(item));
  if (isScalarArray) return <ScalarValue text={summarizedArray(items)} />;
  if (depth >= MAX_DEPTH) return <ScalarValue text={`${items.length} items`} />;
  const shown = items.slice(0, MAX_ARRAY_PREVIEW);
  const rest = items.length - shown.length;
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="font-mono text-xs text-fg-faint">{items.length} items</span>
      {shown.map((item, index) => (
        <div key={index} className="min-w-0 border-l border-border pl-3">
          <KeyValueView value={item} depth={depth + 1} />
        </div>
      ))}
      {rest > 0 && <span className="font-mono text-xs text-fg-faint">…+{rest} more</span>}
    </div>
  );
}
