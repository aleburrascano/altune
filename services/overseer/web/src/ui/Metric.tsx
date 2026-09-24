import type { ReactNode } from "react";
import * as Tooltip from "@radix-ui/react-tooltip";
import type { Severity } from "../types";
import { focusRing } from "./focusRing";

const VALUE_TONES: Record<Severity, string> = {
  ok: "text-ok",
  warn: "text-warn",
  critical: "text-critical",
};

export function StatGrid({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-[repeat(auto-fill,minmax(128px,1fr))] gap-4">{children}</div>;
}

export function Metric({
  label,
  value,
  unit,
  hint,
  tone,
}: {
  label: string;
  value: string | number;
  unit?: string;
  hint?: string;
  tone?: Severity;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="flex items-baseline gap-1">
        <span className={`truncate font-mono text-2xl font-semibold ${tone ? VALUE_TONES[tone] : "text-fg"}`}>
          {value}
        </span>
        {unit ? <span className="font-mono text-sm text-fg-dim">{unit}</span> : null}
      </span>
      <span className="flex items-center gap-1 text-xs uppercase tracking-wider text-fg-faint">
        {label}
        {hint ? <MetricHint label={label} hint={hint} /> : null}
      </span>
    </div>
  );
}

function MetricHint({ label, hint }: { label: string; hint: string }) {
  return (
    <Tooltip.Provider delayDuration={200}>
      <Tooltip.Root>
        <Tooltip.Trigger asChild>
          <button
            type="button"
            aria-label={`About ${label}`}
            className={`inline-flex size-4 items-center justify-center rounded-full border border-border-strong bg-transparent p-0 text-2xs text-fg-dim hover:text-fg ${focusRing}`}
          >
            ?
          </button>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            sideOffset={6}
            className="max-w-64 rounded-md border border-border-strong bg-bg-elev-2 px-2.5 py-1.5 text-sm normal-case tracking-normal text-fg"
          >
            {hint}
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}
