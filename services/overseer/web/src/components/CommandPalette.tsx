import { useEffect, useId, useMemo, useState, type KeyboardEvent } from "react";
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
const activeOptionClass = `flex w-full items-center gap-2.5 px-2.5 py-1.5 text-left text-sm bg-bg-elev-2 text-fg ${focusRing}`;

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
  const [activeIndex, setActiveIndex] = useState(0);
  const navigate = useNavigate();
  const matches = useMemo(() => buckets.filter((s) => matchesQuery(s, query)), [buckets, query]);
  const listboxId = useId();
  const optionId = (id: string) => `${listboxId}-option-${id}`;
  const activeOption = matches[activeIndex];

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  function goToBucket(id: string) {
    navigate(bucketPath(id));
    onOpenChange(false);
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) setQuery("");
    onOpenChange(nextOpen);
  }

  function handleInputKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (matches.length === 0) return;

    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((index) => (index + 1) % matches.length);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((index) => (index - 1 + matches.length) % matches.length);
    } else if (event.key === "Home") {
      event.preventDefault();
      setActiveIndex(0);
    } else if (event.key === "End") {
      event.preventDefault();
      setActiveIndex(matches.length - 1);
    } else if (event.key === "Enter" && activeOption) {
      goToBucket(activeOption.id);
    }
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
          className="fixed left-1/2 top-24 flex w-full max-w-md -translate-x-1/2 flex-col gap-2 border border-border bg-bg-elev p-3"
        >
          <Dialog.Title asChild>
            <h2 className="sr-only">Jump to a bucket</h2>
          </Dialog.Title>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={handleInputKeyDown}
            placeholder="Jump to a bucket…"
            aria-label="Jump to a bucket"
            role="combobox"
            aria-expanded={open}
            aria-controls={listboxId}
            aria-activedescendant={activeOption ? optionId(activeOption.id) : undefined}
            className={`w-full border border-border bg-bg px-3 py-2 text-sm text-fg outline-none ${focusRing}`}
          />
          <ul id={listboxId} className="m-0 mt-2 flex max-h-72 list-none flex-col gap-0.5 overflow-y-auto p-0" role="listbox">
            {matches.map((s, index) => (
              <li key={s.id}>
                <button
                  type="button"
                  id={optionId(s.id)}
                  role="option"
                  aria-selected={index === activeIndex}
                  onClick={() => goToBucket(s.id)}
                  className={index === activeIndex ? activeOptionClass : optionClass}
                >
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
