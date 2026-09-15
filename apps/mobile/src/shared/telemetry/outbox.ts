import { AppState } from 'react-native';

import { ApiError } from '@shared/api-client';

import { loadPersistedOutbox, persistOutbox } from './outboxStore';
import { recordEvent, type DiscoveryEvent } from './recordEvent';

export type OutboxEntry = DiscoveryEvent & {
  event_id: string;
  client_occurred_at: string;
  /** Local-only: the signed-in user who queued the entry. Never sent over the wire. */
  owner_user_id?: string;
};

const MAX_ENTRIES = 50;

export function makeEventId(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
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
      `[telemetry] dropped ${dropped} label-critical outbox ${dropped === 1 ? 'entry' : 'entries'} at cap`,
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
    if (status === 'active') void flushOutbox();
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
  await flushOutbox();
}

function isPermanentlyRejected(error: unknown): boolean {
  return error instanceof ApiError && error.status === 400;
}

export async function flushOutbox(): Promise<void> {
  ensureRestored();
  if (_flushing || _queue.length === 0) return;
  _flushing = true;
  try {
    for (const entry of [..._queue]) {
      // The queue may have been cleared or re-owned by an account switch while a
      // previous send was in flight; never send an entry that is no longer ours.
      if (!_queue.some((e) => e.event_id === entry.event_id) || !ownedByCurrentUser(entry)) {
        continue;
      }
      try {
        await recordEvent(toWire(entry));
      } catch (error) {
        if (!isPermanentlyRejected(error)) break;
      }
      commit(_queue.filter((e) => e.event_id !== entry.event_id));
    }
  } finally {
    _flushing = false;
  }
}

export function clearOutbox(): void {
  _restored = true;
  commit([]);
}

export function _resetOutboxForTest({ restored = true }: { restored?: boolean } = {}): void {
  _queue = [];
  _flushing = false;
  _restored = restored;
  _droppedCritical = 0;
  _owner = null;
}
