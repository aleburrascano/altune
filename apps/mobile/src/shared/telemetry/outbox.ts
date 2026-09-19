import * as Crypto from 'expo-crypto';
import { AppState } from 'react-native';

import { ApiError, NetworkError } from '@shared/api-client';
import { isLoopEnabled, onKillSwitchChange } from '@shared/killSwitch/killSwitch';
import { onSignOut } from '@shared/session/signOutCleanup';

import { loadPersistedOutbox, persistOutbox } from './outboxStore';
import { recordEvent, type DiscoveryEvent } from './recordEvent';

export type OutboxEntry = DiscoveryEvent & {
  event_id: string;
  client_occurred_at: string;
  /** Local-only: the signed-in user who queued the entry. Never sent over the wire. */
  owner_user_id?: string;
};

const MAX_ENTRIES = 50;

/** First retry after a failed flush pass waits between half of this and this. */
export const FLUSH_BACKOFF_BASE_MS = 2_000;
/** No retry ever waits longer than this, however many passes have failed. */
export const FLUSH_BACKOFF_CAP_MS = 5 * 60 * 1000;

// The id must be unguessable, not merely fresh: the server's dedup index on
// event_id is global rather than per-user (migration 006) and inserts with
// ON CONFLICT DO NOTHING, so anyone who can predict an id can claim it first and
// silently swallow the event it belonged to. Math.random's state is recoverable
// from ids already sent, so it cannot mint this (#1774).
export function makeEventId(): string {
  return Crypto.randomUUID();
}

export function withEnvelope(
  event: DiscoveryEvent,
  eventId: string,
  clientOccurredAt: string,
): OutboxEntry {
  return { ...event, event_id: eventId, client_occurred_at: clientOccurredAt };
}

export function dedupeById(entries: readonly OutboxEntry[]): OutboxEntry[] {
  const byId = new Map<string, OutboxEntry>();
  for (const e of entries) byId.set(e.event_id, e);
  return [...byId.values()];
}

export function capEntries(entries: readonly OutboxEntry[], max: number): OutboxEntry[] {
  return entries.length <= max ? [...entries] : entries.slice(entries.length - max);
}

let _queue: OutboxEntry[] = [];
let _flushing = false;
let _listening = false;
let _restored = false;
let _droppedCritical = 0;
// The flush loop's own retry schedule. A pass that leaves a retryable failure in
// the queue bumps the counter and arms a timer; until it fires, the enqueue and
// foreground triggers do not start a pass, so a chronically failing entry is
// retried on a capped, jittered schedule rather than on every trigger.
let _failedPasses = 0;
let _retryAt = 0;
let _retryTimer: ReturnType<typeof setTimeout> | null = null;
// The signed-in user the outbox currently sends for (set by useSession). Entries
// are tagged with it at enqueue and only ever flushed while it still matches, so
// a queued event can never ride on another account's bearer token.
let _owner: string | null = null;

export function droppedCriticalCount(): number {
  return _droppedCritical;
}

function capCritical(entries: readonly OutboxEntry[]): OutboxEntry[] {
  const capped = capEntries(entries, MAX_ENTRIES);
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
  AppState.addEventListener('change', (status) => {
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

/**
 * Sets the user the outbox sends for. When a user is signed in, entries that do
 * not belong to them (another account's, or untagged ones queued with nobody
 * signed in) are dropped before any flush can send them under this user's token.
 */
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
  const entry = withOwner(withEnvelope(event, makeEventId(), new Date().toISOString()), _owner);
  commit(capCritical(dedupeById([..._queue, entry])));
  await requestFlush();
}

/**
 * Delay before retry number `failedPasses` (1-based): exponential from
 * FLUSH_BACKOFF_BASE_MS, capped at FLUSH_BACKOFF_CAP_MS, with equal jitter so the
 * wait lands in [ceiling/2, ceiling]. `random` is a sample in [0, 1).
 */
export function flushBackoffMs(failedPasses: number, random: number): number {
  const exponent = Math.min(Math.max(failedPasses, 1) - 1, 30);
  const ceiling = Math.min(FLUSH_BACKOFF_CAP_MS, FLUSH_BACKOFF_BASE_MS * 2 ** exponent);
  return Math.round(ceiling / 2 + random * (ceiling / 2));
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
  console.warn(
    `[telemetry] outbox flush left ${_queue.length} queued; retry ${_failedPasses} in ${delay}ms; ${_droppedCritical} dropped at cap since launch`,
  );
}

// The trigger path (enqueue, foreground). It honours the backoff window, so no
// trigger can shortcut the retry schedule the flush loop owns.
function requestFlush(): Promise<void> {
  if (_retryAt > Date.now()) return Promise.resolve();
  return flushOutbox();
}

function isPermanentlyRejected(error: unknown): boolean {
  return error instanceof ApiError && error.status === 400;
}

// sent/dropped leave the queue; retry stays queued and the pass moves on; offline
// means the transport itself is down, so every other entry would fail the same
// way and the pass stops instead of burning one request per queued entry.
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

// The queue may have been cleared or re-owned by an account switch while a
// previous send was in flight; never send an entry that is no longer ours.
function stillOurs(entry: OutboxEntry): boolean {
  return _queue.some((e) => e.event_id === entry.event_id) && ownedByCurrentUser(entry);
}

const flushEnabled = (): boolean => isLoopEnabled('telemetryFlush');

/**
 * Runs one send pass over the queue now. A failure on one entry is logged and the
 * pass moves on to the entries behind it (unless the transport is down), so one
 * persistently failing entry never starves the rest. A pass that leaves anything
 * retryable queued arms the backoff timer; a clean pass resets it.
 *
 * With the telemetry kill switch off nothing is sent: a pass does not start, a
 * running one stops before its next entry, and no retry is armed. Entries stay
 * queued (and persisted) until the switch is turned back on.
 */
export async function flushOutbox(): Promise<void> {
  ensureRestored();
  if (_flushing || _queue.length === 0) return;
  if (!flushEnabled()) {
    resetBackoff();
    return;
  }
  _flushing = true;
  let retryable = false;
  try {
    for (const entry of [..._queue]) {
      if (!flushEnabled()) break;
      if (!stillOurs(entry)) continue;
      const outcome = await send(entry);
      if (outcome === 'sent' || outcome === 'dropped') {
        commit(_queue.filter((e) => e.event_id !== entry.event_id));
        continue;
      }
      retryable = true;
      if (outcome === 'offline') break;
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

export function clearOutbox(): void {
  _restored = true;
  resetBackoff();
  commit([]);
}

// Queued entries carry the previous account's activity and would be persisted
// across the switch; setOutboxOwner only filters what a later flush may send.
onSignOut(clearOutbox);

export function _resetOutboxForTest({ restored = true }: { restored?: boolean } = {}): void {
  _queue = [];
  _flushing = false;
  _restored = restored;
  _droppedCritical = 0;
  _owner = null;
  resetBackoff();
}
