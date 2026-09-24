import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import * as Dialog from "@radix-ui/react-dialog";
import type { Snapshot } from "../types";
import { bucketPath } from "../routes";
import { focusRing } from "../ui/focusRing";

function matchesQuery(snapshot: Snapshot, query: string): boolean {
  const normalized = query.trim().toLowerCase();
  if (normalized === "") return true;
  return snapshot.title.toLowerCase().includes(normalized) || snapshot.id.toLowerCase().includes(normalized);
}

const optionClass = `flex w-full items-center gap-2.5 px-2.5 py-1.5 text-left text-sm text-fg-dim hover:bg-bg-elev-2 hover:text-fg ${focusRing}`;

export function CommandPalette({
  buckets,
  open,
  onOpenChange,
  restoreFocusTo,
}: {
  buckets: Snapshot[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  restoreFocusTo: () => void;
}) {
  const [query, setQuery] = useState("");
  const navigate = useNavigate();
  const matches = useMemo(() => buckets.filter((s) => matchesQuery(s, query)), [buckets, query]);

  function goToBucket(id: string) {
    navigate(bucketPath(id));
    onOpenChange(false);
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) setQuery("");
    onOpenChange(nextOpen);
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-bg/70" />
        <Dialog.Content
          aria-describedby={undefined}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            restoreFocusTo();
          }}
          className="fixed left-1/2 top-24 w-full max-w-md -translate-x-1/2 flex-col gap-2 border border-border bg-bg-elev p-3"
        >
          <Dialog.Title asChild>
            <h2 className="sr-only">Jump to a bucket</h2>
          </Dialog.Title>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && matches[0]) goToBucket(matches[0].id);
            }}
            placeholder="Jump to a bucket…"
            aria-label="Jump to a bucket"
            className={`w-full border border-border bg-bg px-3 py-2 text-sm text-fg outline-none ${focusRing}`}
          />
          <ul className="m-0 mt-2 flex max-h-72 list-none flex-col gap-0.5 overflow-y-auto p-0" role="listbox">
            {matches.map((s) => (
              <li key={s.id}>
                <button type="button" role="option" aria-selected={false} onClick={() => goToBucket(s.id)} className={optionClass}>
                  <span aria-hidden="true" className={`dot dot-sev-${s.severity}`} />
                  <span className="truncate">{s.title}</span>
                </button>
              </li>
            ))}
            {matches.length === 0 ? (
              <li className="px-2.5 py-1.5 text-sm text-fg-faint">No matching bucket</li>
            ) : null}
          </ul>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
