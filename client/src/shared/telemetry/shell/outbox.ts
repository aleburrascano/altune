import { ApiError, NetworkError } from '@shared/failure';
import { clampedExponent, equalJitterMs } from '@shared/timing';

import type { DiscoveryEvent } from '@shared/telemetry/core/discoveryEvent';
import {
  capOutbox,
  dedupeById,
  withEnvelope,
  type OutboxEntry,
} from '@shared/telemetry/core/outboxEntries';
import type {
  OutboxStore,
  TelemetryAppState,
  TelemetryClock,
  TelemetryLog,
} from '@shared/telemetry/ports/telemetry';
import type { RecordEvent } from '@shared/telemetry/shell/recordEvent';

const MAX_ENTRIES = 50;
const MAX_DIAGNOSTIC_ENTRIES = 20;

const FLUSH_BACKOFF_BASE_MS = 2_000;
const FLUSH_BACKOFF_CAP_MS = 300_000;

const RETRYABLE_CLIENT_STATUSES: ReadonlySet<number> = new Set([401, 408, 429]);
const FIRST_CLIENT_ERROR_STATUS = 400;
const FIRST_SERVER_ERROR_STATUS = 500;

type OutboxDeps = {
  clock: TelemetryClock;
  store: OutboxStore;
  appState: TelemetryAppState;
  log: TelemetryLog;
  recordEvent: RecordEvent;
  onSignOut: (listener: () => void) => () => void;
};

type SendOutcome = 'sent' | 'dropped' | 'retry' | 'offline';

function flushBackoffMs(failedPasses: number, random: number): number {
  const exponent = clampedExponent(failedPasses, 1) - 1;
  return equalJitterMs(FLUSH_BACKOFF_BASE_MS, FLUSH_BACKOFF_CAP_MS, exponent, random);
}

function isPermanentlyRejected(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false;
  const clientError =
    error.status >= FIRST_CLIENT_ERROR_STATUS && error.status < FIRST_SERVER_ERROR_STATUS;
  return clientError && !RETRYABLE_CLIENT_STATUSES.has(error.status);
}

function withOwner(entry: OutboxEntry, owner: string | null): OutboxEntry {
  return owner === null ? entry : { ...entry, owner_user_id: owner };
}

function toWire(entry: OutboxEntry): DiscoveryEvent {
  const wire: OutboxEntry = { ...entry };
  delete wire.owner_user_id;
  return wire;
}

export function createOutbox(deps: OutboxDeps) {
  const { clock, store, appState, log, recordEvent, onSignOut } = deps;
  let queue: OutboxEntry[] = [];
  let flushing = false;
  let listening = false;
  let restored = false;
  let droppedCritical = 0;
  let failedPasses = 0;
  let retryAt = 0;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let owner: string | null = null;
  let disposed = false;
  const unsubscribes: (() => void)[] = [];
  const flushes = new Set<Promise<void>>();

  function capCritical(entries: readonly OutboxEntry[]): OutboxEntry[] {
    const capped = capOutbox(entries, MAX_ENTRIES, MAX_DIAGNOSTIC_ENTRIES);
    const dropped = entries.length - capped.length;
    if (dropped > 0) {
      droppedCritical += dropped;
      log.warn(
        `[telemetry] dropped ${dropped} label-critical outbox ${dropped === 1 ? 'entry' : 'entries'} at cap (${droppedCritical} since launch)`,
      );
    }
    return capped;
  }

  function ensureRestored(): void {
    if (restored) return;
    restored = true;
    queue = capCritical(dedupeById([...store.load(), ...queue]));
  }

  function commit(next: OutboxEntry[]): void {
    if (disposed) return;
    queue = next;
    store.save(queue);
  }

  function ownedByCurrentUser(entry: OutboxEntry): boolean {
    return (entry.owner_user_id ?? null) === owner;
  }

  function cancelRetry(): void {
    if (retryTimer !== null) clearTimeout(retryTimer);
    retryTimer = null;
    retryAt = 0;
  }

  function resetBackoff(): void {
    cancelRetry();
    failedPasses = 0;
  }

  function scheduleRetry(): void {
    cancelRetry();
    if (disposed) return;
    failedPasses += 1;
    const delay = flushBackoffMs(failedPasses, clock.random());
    retryAt = clock.now() + delay;
    retryTimer = setTimeout(() => {
      retryTimer = null;
      retryAt = 0;
      void flushOutbox();
    }, delay);
    recordEvent({
      type: 'outbox_flush_failed',
      payload: {
        failed_passes: failedPasses,
        queued: queue.length,
        dropped_at_cap: droppedCritical,
      },
    }).catch((error: unknown) => {
      log.warn('[telemetry] recording event failed', error);
    });
    log.warn(
      `[telemetry] outbox flush left ${queue.length} queued; retry ${failedPasses} in ${delay}ms; ${droppedCritical} dropped at cap since launch`,
    );
  }

  function requestFlush(): Promise<void> {
    if (retryAt > clock.now()) return Promise.resolve();
    return flushOutbox();
  }

  function ensureFlushOnForeground(): void {
    if (listening || disposed) return;
    listening = true;
    unsubscribes.push(
      appState.subscribe((status) => {
        if (status === 'active') void requestFlush();
      }),
    );
  }

  function classifyFailure(entry: OutboxEntry, error: unknown): SendOutcome {
    const label = `${entry.type} ${entry.event_id}`;
    if (isPermanentlyRejected(error)) {
      log.warn(`[telemetry] outbox dropping ${label}, rejected by the server`, error);
      return 'dropped';
    }
    log.warn(`[telemetry] outbox send failed for ${label}`, error);
    return error instanceof NetworkError ? 'offline' : 'retry';
  }

  async function send(entry: OutboxEntry): Promise<SendOutcome> {
    try {
      await recordEvent(toWire(entry));
      return 'sent';
    } catch (error) {
      return classifyFailure(entry, error);
    }
  }

  function stillOurs(entry: OutboxEntry): boolean {
    return queue.some((e) => e.event_id === entry.event_id) && ownedByCurrentUser(entry);
  }

  function flushOutbox(): Promise<void> {
    const run = flushPass();
    flushes.add(run);
    return run.finally(() => flushes.delete(run));
  }

  async function flushPass(): Promise<void> {
    ensureRestored();
    if (isDisposed() || flushing || queue.length === 0) return;
    flushing = true;
    let retryable = false;
    const attempted = new Set<string>();
    try {
      let halted = false;
      while (!halted) {
        const batch = queue.filter((e) => !attempted.has(e.event_id));
        if (batch.length === 0) break;
        for (const entry of batch) {
          attempted.add(entry.event_id);
          if (isDisposed()) {
            halted = true;
            break;
          }
          if (!stillOurs(entry)) continue;
          const outcome = await send(entry);
          if (outcome === 'sent' || outcome === 'dropped') {
            commit(queue.filter((e) => e.event_id !== entry.event_id));
            continue;
          }
          retryable = true;
          if (outcome === 'offline') {
            halted = true;
            break;
          }
        }
      }
    } finally {
      flushing = false;
    }
    if (retryable && queue.length > 0) scheduleRetry();
    else resetBackoff();
  }

  function setOwner(userId: string | null): void {
    ensureRestored();
    owner = userId;
    if (userId === null) return;
    const kept = queue.filter(ownedByCurrentUser);
    if (kept.length !== queue.length) commit(kept);
  }

  async function enqueueCritical(event: DiscoveryEvent): Promise<void> {
    ensureRestored();
    ensureFlushOnForeground();
    const stamped = withEnvelope(event, clock.newId(), new Date(clock.now()).toISOString());
    commit(capCritical(dedupeById([...queue, withOwner(stamped, owner)])));
    await requestFlush();
  }

  unsubscribes.push(
    onSignOut(() => {
      restored = true;
      resetBackoff();
      commit([]);
    }),
  );

  function isDisposed(): boolean {
    return disposed;
  }

  function dispose(): void {
    disposed = true;
    cancelRetry();
    unsubscribes.splice(0).forEach((unsubscribe) => unsubscribe());
  }

  async function settled(): Promise<void> {
    await Promise.allSettled([...flushes]);
  }

  return { enqueueCritical, flushOutbox, setOwner, dispose, settled };
}
