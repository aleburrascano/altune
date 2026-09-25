import type { ReactNode } from "react";

export type NoticeKind = "stale" | "down" | "empty" | "lossy" | "auth";

const NOTICE_TONES: Record<NoticeKind, string> = {
  stale: "border-warn/30 bg-warn/5 text-warn",
  down: "border-critical/30 bg-critical/5 text-critical",
  empty: "border-border text-fg-faint italic",
  lossy: "border-warn/30 bg-warn/5 text-warn",
  auth: "border-warn/30 bg-warn/5 text-warn",
};

export function Notice({ kind, children }: { kind: NoticeKind; children: ReactNode }) {
  return (
    <p role="status" data-kind={kind} className={`m-0 rounded-md border px-3 py-2 text-sm ${NOTICE_TONES[kind]}`}>
      {children}
    </p>
  );
}
