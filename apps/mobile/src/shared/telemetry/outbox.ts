/**
 * outbox — at-least-once delivery for the label-critical event tier.
 *
 * library_add and wrong_album are the highest-value signals: a lost one is a lost
 * relevance label. They go through this outbox with a client-minted idempotency
 * key (event_id) and are retried until the POST succeeds; the server dedups on
 * event_id, so the retry is safe. Everything else stays fire-and-forget.
 *
 * The queue is in-memory and flushed on enqueue + on app foreground (a reconnect
 * proxy). Durability across a hard app-kill while offline is intentionally NOT
 * covered here: it needs a durable client store (AsyncStorage / SQLite), which is
 * not a current dependency — adding one is an ADR. The library WRITE itself
 * (createTrack) is already durable server-side; this protects the telemetry label.
 *
 * Pure helpers (withEnvelope, dedupeById, capEntries) hold the queue logic and
 * are unit-tested; the stateful wrapper is a thin shell.
 */

import { AppState } from 'react-native';

import { recordEvent, type DiscoveryEvent } from './recordEvent';

export type OutboxEntry = DiscoveryEvent & {
  event_id: string;
  client_occurred_at: string;
};

const MAX_ENTRIES = 50;

// makeEventId is an RFC4122 v4 id — an idempotency key, not a security token, so
// Math.random is fine. The server stores it as a UUID and dedups on it.
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

// dedupeById keeps the last entry per event_id (a re-enqueue supersedes).
export function dedupeById(entries: readonly OutboxEntry[]): OutboxEntry[] {
  const byId = new Map<string, OutboxEntry>();
  for (const e of entries) byId.set(e.event_id, e);
  return [...byId.values()];
}

// capEntries bounds the queue, dropping the oldest beyond max.
export function capEntries(entries: readonly OutboxEntry[], max: number): OutboxEntry[] {
  return entries.length <= max ? [...entries] : entries.slice(entries.length - max);
}

let _queue: OutboxEntry[] = [];
let _flushing = false;
let _listening = false;

function ensureFlushOnForeground(): void {
  if (_listening) return;
  _listening = true;
  AppState.addEventListener('change', (status) => {
    if (status === 'active') void flushOutbox();
  });
}

export async function enqueueCritical(event: DiscoveryEvent): Promise<void> {
  ensureFlushOnForeground();
  const entry = withEnvelope(event, makeEventId(), new Date().toISOString());
  _queue = capEntries(dedupeById([..._queue, entry]), MAX_ENTRIES);
  await flushOutbox();
}

// flushOutbox drains the queue, removing each entry only once its POST succeeds.
// On the first failure (likely offline) it stops and leaves the rest for the next
// trigger (enqueue or foreground). Never throws — best-effort like all telemetry.
export async function flushOutbox(): Promise<void> {
  if (_flushing || _queue.length === 0) return;
  _flushing = true;
  try {
    for (const entry of [..._queue]) {
      try {
        await recordEvent(entry);
        _queue = _queue.filter((e) => e.event_id !== entry.event_id);
      } catch {
        break;
      }
    }
  } finally {
    _flushing = false;
  }
}

// _resetOutboxForTest clears module state between tests.
export function _resetOutboxForTest(): void {
  _queue = [];
  _flushing = false;
}
