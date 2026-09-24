import { useCallback, useRef, useState, type ReactNode } from "react";
import { NavLink, useLocation, useMatch, useNavigate } from "react-router-dom";
import * as Dialog from "@radix-ui/react-dialog";
import type { Snapshot } from "../types";
import { bucketPath, overviewPath } from "../routes";
import { focusRing } from "../ui/focusRing";
import { neighborBucketId } from "../lib/order";
import { useShortcuts } from "../hooks/useShortcuts";
import { Nav } from "./Nav";
import { CommandPalette } from "./CommandPalette";

const iconButton = `inline-flex size-8 shrink-0 items-center justify-center border border-border bg-transparent text-fg-dim hover:bg-bg-elev-2 hover:text-fg ${focusRing}`;

function Brand({ isCollapsed = false }: { isCollapsed?: boolean }) {
  return (
    <NavLink to={overviewPath} end className={`flex min-w-0 items-center gap-2 font-semibold text-fg ${focusRing}`}>
      <span aria-hidden="true" className="text-lg text-accent">
        ◆
      </span>
      <span className={isCollapsed ? "sr-only" : "truncate"}>Overseer</span>
    </NavLink>
  );
}

function Account({
  status,
  email,
  onSignOut,
  isCollapsed = false,
}: {
  status: ReactNode;
  email: string;
  onSignOut: () => void;
  isCollapsed?: boolean;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5 border-t border-border pt-3 text-sm text-fg-faint">
      {status}
      <span className={isCollapsed ? "sr-only" : "truncate text-fg-dim"}>{email}</span>
      <button
        type="button"
        onClick={onSignOut}
        className={`self-start border-0 bg-transparent p-0 text-left text-sm text-accent ${focusRing}`}
      >
        Sign out
      </button>
    </div>
  );
}

function PaletteHint({ onOpen, isCollapsed = false }: { onOpen: () => void; isCollapsed?: boolean }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      title={isCollapsed ? "Jump to a bucket (Ctrl/Cmd K)" : undefined}
      className={`flex min-w-0 items-center border-0 bg-transparent p-0 text-xs text-fg-faint hover:text-fg ${focusRing} ${isCollapsed ? "justify-center" : "justify-between gap-2"}`}
    >
      <span className={isCollapsed ? "sr-only" : "truncate"}>Jump to a bucket</span>
      <kbd aria-hidden="true" className="border border-border px-1 py-0.5 font-mono text-2xs text-fg-dim">
        ⌘K
      </kbd>
    </button>
  );
}

function Drawer({
  buckets,
  status,
  email,
  onSignOut,
}: {
  buckets: Snapshot[];
  status: ReactNode;
  email: string;
  onSignOut: () => void;
}) {
  const location = useLocation();
  const [openedAtKey, setOpenedAtKey] = useState<string | null>(null);
  const isOpen = openedAtKey === location.key;

  return (
    <Dialog.Root open={isOpen} onOpenChange={(open) => setOpenedAtKey(open ? location.key : null)}>
      <Dialog.Trigger aria-label="Open navigation" className={iconButton}>
        <span aria-hidden="true">☰</span>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-bg/70 md:hidden" />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed inset-y-0 left-0 flex w-72 max-w-[85vw] flex-col gap-4 border-r border-border bg-bg-elev p-4 md:hidden"
        >
          <div className="flex items-center justify-between gap-3">
            <Dialog.Title asChild>
              <h2 className="m-0 min-w-0 text-base">
                <Brand />
              </h2>
            </Dialog.Title>
            <Dialog.Close aria-label="Close navigation" className={iconButton}>
              <span aria-hidden="true">✕</span>
            </Dialog.Close>
          </div>
          <Nav buckets={buckets} />
          <Account status={status} email={email} onSignOut={onSignOut} />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function Shell({
  buckets,
  status,
  user,
  onSignOut,
  children,
}: {
  buckets: Snapshot[];
  status: ReactNode;
  user: { email: string };
  onSignOut: () => void;
  children: ReactNode;
}) {
  const [isCollapsed, setIsCollapsed] = useState(false);
  const [isPaletteOpen, setIsPaletteOpen] = useState(false);
  const paletteOpenerRef = useRef<HTMLElement | null>(null);
  const navigate = useNavigate();
  const bucketMatch = useMatch("/bucket/:id");
  const currentBucketId = bucketMatch?.params.id;

  const openPalette = useCallback(() => {
    paletteOpenerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setIsPaletteOpen(true);
  }, []);

  const restoreFocusToOpener = useCallback(() => {
    if (paletteOpenerRef.current?.isConnected) paletteOpenerRef.current.focus();
  }, []);

  const goToNeighbor = useCallback(
    (step: 1 | -1) => {
      const id = neighborBucketId(buckets, currentBucketId, step);
      if (id) navigate(bucketPath(id));
    },
    [buckets, currentBucketId, navigate],
  );

  useShortcuts({
    openPalette,
    nextBucket: () => goToNeighbor(1),
    previousBucket: () => goToNeighbor(-1),
  });

  return (
    <div className="flex h-dvh min-w-0 flex-col md:flex-row">
      <header className="flex min-w-0 items-center gap-3 border-b border-border bg-bg-elev px-4 py-2.5 md:hidden">
        <Drawer buckets={buckets} status={status} email={user.email} onSignOut={onSignOut} />
        <Brand />
        <button
          type="button"
          aria-label="Jump to a bucket"
          onClick={openPalette}
          className={`ml-auto shrink-0 ${iconButton}`}
        >
          <span aria-hidden="true">⌕</span>
        </button>
        <div className="shrink-0">{status}</div>
      </header>
      <aside
        className={`hidden min-h-0 shrink-0 flex-col gap-4 border-r border-border bg-bg-elev p-3 md:flex ${isCollapsed ? "md:w-16" : "md:w-60"}`}
      >
        <div className={`flex min-w-0 items-center gap-2 ${isCollapsed ? "flex-col" : "justify-between"}`}>
          <Brand isCollapsed={isCollapsed} />
          <button
            type="button"
            aria-label={isCollapsed ? "Expand navigation" : "Collapse navigation"}
            aria-expanded={!isCollapsed}
            onClick={() => setIsCollapsed((was) => !was)}
            className={iconButton}
          >
            <span aria-hidden="true">{isCollapsed ? "»" : "«"}</span>
          </button>
        </div>
        <Nav buckets={buckets} isCollapsed={isCollapsed} />
        <PaletteHint onOpen={openPalette} isCollapsed={isCollapsed} />
        <Account status={status} email={user.email} onSignOut={onSignOut} isCollapsed={isCollapsed} />
      </aside>
      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto p-4 md:p-6">{children}</main>
      <CommandPalette
        buckets={buckets}
        open={isPaletteOpen}
        onOpenChange={setIsPaletteOpen}
        restoreFocusTo={restoreFocusToOpener}
      />
    </div>
  );
}
