import { randomUuid } from '@shared/id/uuid';
import { subscribeAppState } from '@shared/lifecycle/appState';

import { ApiError, NetworkError } from '@shared/apiClient';
import { clampedExponent, equalJitterMs } from '@shared/timing/backoff';
import { isLoopEnabled, onKillSwitchChange } from '@shared/killSwitch/killSwitch';
import { onSignOut } from '@shared/session/signOutCleanup';

import { loadPersistedOutbox, persistOutbox } from '@shared/telemetry/outboxStore';
import {
  capOutbox,
  dedupeById,
  withEnvelope,
  type OutboxEntry,
} from '@shared/telemetry/outboxEntries';
import { recordEvent, type DiscoveryEvent } from '@shared/telemetry/recordEvent';

const MAX_ENTRIES = 50;
const MAX_DIAGNOSTIC_ENTRIES = 20;

const FLUSH_BACKOFF_BASE_MS = 2_000;
const FLUSH_BACKOFF_CAP_MS = 300_000;

let _queue: OutboxEntry[] = [];
let _flushing = false;
let _listening = false;
let _restored = false;
let _droppedCritical = 0;
let _failedPasses = 0;
let _retryAt = 0;
let _retryTimer: ReturnType<typeof setTimeout> | null = null;
let _owner: string | null = null;

function capCritical(entries: readonly OutboxEntry[]): OutboxEntry[] {
  const capped = capOutbox(entries, MAX_ENTRIES, MAX_DIAGNOSTIC_ENTRIES);
  const dropped = entries.length - capped.length;
  if (dropped > 0) {
    _droppedCritical += dropped;
    console.warn(
      `[telemetry] dropped ${dropped} label-critical outbox ${dropped === 1 ? 'entry' : 'entries'} at cap (${_droppedCritical} since launch)`,
    );
  }
  return capped;
}

function ensureRestored(): void {
  if (_restored) return;
  _restored = true;
  _queue = capCritical(dedupeById([...loadPersistedOutbox(), ..._queue]));
}

function commit(next: OutboxEntry[]): void {
  _queue = next;
  persistOutbox(_queue);
}

function ensureFlushOnForeground(): void {
  if (_listening) return;
  _listening = true;
  subscribeAppState((status) => {
    if (status === 'active') void requestFlush();
  });
}

function withOwner(entry: OutboxEntry, owner: string | null): OutboxEntry {
  return owner === null ? entry : { ...entry, owner_user_id: owner };
}

function ownedByCurrentUser(entry: OutboxEntry): boolean {
  return (entry.owner_user_id ?? null) === _owner;
}

function toWire(entry: OutboxEntry): DiscoveryEvent {
  const wire: OutboxEntry = { ...entry };
  delete wire.owner_user_id;
  return wire;
}

export function setOutboxOwner(userId: string | null): void {
  ensureRestored();
  _owner = userId;
  if (userId === null) return;
  const kept = _queue.filter(ownedByCurrentUser);
  if (kept.length !== _queue.length) commit(kept);
}

export async function enqueueCritical(event: DiscoveryEvent): Promise<void> {
  ensureRestored();
  ensureFlushOnForeground();
  const entry = withOwner(withEnvelope(event, randomUuid(), new Date().toISOString()), _owner);
  commit(capCritical(dedupeById([..._queue, entry])));
  await requestFlush();
}

function flushBackoffMs(failedPasses: number, random: number): number {
  const exponent = clampedExponent(failedPasses, 1) - 1;
  return equalJitterMs(FLUSH_BACKOFF_BASE_MS, FLUSH_BACKOFF_CAP_MS, exponent, random);
}

function cancelRetry(): void {
  if (_retryTimer !== null) clearTimeout(_retryTimer);
  _retryTimer = null;
  _retryAt = 0;
}

function resetBackoff(): void {
  cancelRetry();
  _failedPasses = 0;
}

function scheduleRetry(): void {
  cancelRetry();
  _failedPasses += 1;
  const delay = flushBackoffMs(_failedPasses, Math.random());
  _retryAt = Date.now() + delay;
  _retryTimer = setTimeout(() => {
    _retryTimer = null;
    _retryAt = 0;
    void flushOutbox();
  }, delay);
  recordEvent({
    type: 'outbox_flush_failed',
    payload: {
      failed_passes: _failedPasses,
      queued: _queue.length,
      dropped_at_cap: _droppedCritical,
    },
  }).catch((error: unknown) => {
    console.warn('[telemetry] recording event failed', error);
  });
  console.warn(
    `[telemetry] outbox flush left ${_queue.length} queued; retry ${_failedPasses} in ${delay}ms; ${_droppedCritical} dropped at cap since launch`,
  );
}

function requestFlush(): Promise<void> {
  if (_retryAt > Date.now()) return Promise.resolve();
  return flushOutbox();
}

const RETRYABLE_CLIENT_STATUSES: ReadonlySet<number> = new Set([401, 408, 429]);

function isPermanentlyRejected(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false;
  const clientError = error.status >= 400 && error.status < 500;
  return clientError && !RETRYABLE_CLIENT_STATUSES.has(error.status);
}

type SendOutcome = 'sent' | 'dropped' | 'retry' | 'offline';

function classifyFailure(entry: OutboxEntry, error: unknown): SendOutcome {
  const label = `${entry.type} ${entry.event_id}`;
  if (isPermanentlyRejected(error)) {
    console.warn(`[telemetry] outbox dropping ${label}, rejected by the server`, error);
    return 'dropped';
  }
  console.warn(`[telemetry] outbox send failed for ${label}`, error);
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
  return _queue.some((e) => e.event_id === entry.event_id) && ownedByCurrentUser(entry);
}

function flushEnabled(): boolean {
  return isLoopEnabled('telemetryFlush');
}

export async function flushOutbox(): Promise<void> {
  ensureRestored();
  if (_flushing || _queue.length === 0) return;
  if (!flushEnabled()) {
    resetBackoff();
    return;
  }
  _flushing = true;
  let retryable = false;
  const attempted = new Set<string>();
  try {
    let halted = false;
    while (!halted) {
      const batch = _queue.filter((e) => !attempted.has(e.event_id));
      if (batch.length === 0) break;
      for (const entry of batch) {
        if (!flushEnabled()) {
          halted = true;
          break;
        }
        attempted.add(entry.event_id);
        if (!stillOurs(entry)) continue;
        const outcome = await send(entry);
        if (outcome === 'sent' || outcome === 'dropped') {
          commit(_queue.filter((e) => e.event_id !== entry.event_id));
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
    _flushing = false;
  }
  if (retryable && _queue.length > 0) scheduleRetry();
  else resetBackoff();
}

onKillSwitchChange((loop, enabled) => {
  if (loop === 'telemetryFlush' && enabled) void flushOutbox();
});

function clearOutbox(): void {
  _restored = true;
  resetBackoff();
  commit([]);
}

onSignOut(clearOutbox);

export function _resetOutboxForTest({ restored = true }: { restored?: boolean } = {}): void {
  _queue = [];
  _flushing = false;
  _restored = restored;
  _droppedCritical = 0;
  _owner = null;
  resetBackoff();
}
