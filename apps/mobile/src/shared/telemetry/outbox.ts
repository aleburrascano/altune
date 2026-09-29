import * as Crypto from 'expo-crypto';
import { AppState } from 'react-native';

import { ApiError, NetworkError } from '@shared/api-client';
import { equalJitterMs, isTelemetryGated } from '@shared/errors';
import { isLoopEnabled, onKillSwitchChange } from '@shared/killSwitch/killSwitch';
import { onSignOut } from '@shared/session/signOutCleanup';

import { loadPersistedOutbox, persistOutbox } from './outboxStore';
import { recordEvent, type DiscoveryEvent } from './recordEvent';

export type OutboxEntry = DiscoveryEvent & {
  event_id: string;
  client_occurred_at: string;
  owner_user_id?: string;
};

const MAX_ENTRIES = 50;
const MAX_DIAGNOSTIC_ENTRIES = 20;
const DIAGNOSTIC_TYPES: ReadonlySet<DiscoveryEvent['type']> = new Set([
  'acquisition_ui',
  'client_error',
]);

export const FLUSH_BACKOFF_BASE_MS = 2_000;
export const FLUSH_BACKOFF_CAP_MS = 5 * 60 * 1000;

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

function isDiagnostic(entry: OutboxEntry): boolean {
  return DIAGNOSTIC_TYPES.has(entry.type);
}

function keptIds(entries: readonly OutboxEntry[], max: number, maxDiagnostic: number): Set<string> {
  const labels = capEntries(
    entries.filter((e) => !isDiagnostic(e)),
    max,
  );
  const diagnosticRoom = Math.min(maxDiagnostic, max - labels.length);
  const diagnostics = capEntries(entries.filter(isDiagnostic), Math.max(0, diagnosticRoom));
  return new Set([...labels, ...diagnostics].map((e) => e.event_id));
}

export function capOutbox(
  entries: readonly OutboxEntry[],
  max: number,
  maxDiagnostic: number,
): OutboxEntry[] {
  const kept = keptIds(entries, max, maxDiagnostic);
  return entries.filter((e) => kept.has(e.event_id));
}

let _queue: OutboxEntry[] = [];
let _flushing = false;
let _listening = false;
let _restored = false;
let _droppedCritical = 0;
let _failedPasses = 0;
let _retryAt = 0;
let _retryTimer: ReturnType<typeof setTimeout> | null = null;
let _owner: string | null = null;

export function droppedCriticalCount(): number {
  return _droppedCritical;
}

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

export function flushBackoffMs(failedPasses: number, random: number): number {
  const exponent = Math.min(Math.max(failedPasses, 1) - 1, 30);
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
  }).catch(() => undefined);
  console.warn(
    `[telemetry] outbox flush left ${_queue.length} queued; retry ${_failedPasses} in ${delay}ms; ${_droppedCritical} dropped at cap since launch`,
  );
}

function requestFlush(): Promise<void> {
  if (_retryAt > Date.now()) return Promise.resolve();
  return flushOutbox();
}

function isPermanentlyRejected(error: unknown): boolean {
  return error instanceof ApiError && error.status === 400;
}

type SendOutcome = 'sent' | 'dropped' | 'retry' | 'offline' | 'gated';

function classifyFailure(entry: OutboxEntry, error: unknown): SendOutcome {
  if (isTelemetryGated(error)) return 'gated';
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

const flushEnabled = (): boolean => isLoopEnabled('telemetryFlush');

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
      if (outcome === 'gated') break;
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

onSignOut(clearOutbox);

export function _resetOutboxForTest({ restored = true }: { restored?: boolean } = {}): void {
  _queue = [];
  _flushing = false;
  _restored = restored;
  _droppedCritical = 0;
  _owner = null;
  resetBackoff();
}
