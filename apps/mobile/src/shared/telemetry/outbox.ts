import { AppState } from 'react-native';

import { ApiError } from '@shared/api-client';

import { loadPersistedOutbox, persistOutbox } from './outboxStore';
import { recordEvent, type DiscoveryEvent } from './recordEvent';

export type OutboxEntry = DiscoveryEvent & {
  event_id: string;
  client_occurred_at: string;
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

function ensureRestored(): void {
  if (_restored) return;
  _restored = true;
  _queue = capEntries(dedupeById([...loadPersistedOutbox(), ..._queue]), MAX_ENTRIES);
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

export async function enqueueCritical(event: DiscoveryEvent): Promise<void> {
  ensureRestored();
  ensureFlushOnForeground();
  const entry = withEnvelope(event, makeEventId(), new Date().toISOString());
  commit(capEntries(dedupeById([..._queue, entry]), MAX_ENTRIES));
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
      try {
        await recordEvent(entry);
      } catch (error) {
        if (!isPermanentlyRejected(error)) break;
      }
      commit(_queue.filter((e) => e.event_id !== entry.event_id));
    }
  } finally {
    _flushing = false;
  }
}

export function _resetOutboxForTest(): void {
  _queue = [];
  _flushing = false;
  _restored = true;
}
