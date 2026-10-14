import type { DiscoveryEvent } from '@shared/telemetry/recordEvent';

export type OutboxEntry = DiscoveryEvent & {
  event_id: string;
  client_occurred_at: string;
  owner_user_id?: string;
};

const DIAGNOSTIC_TYPES: ReadonlySet<DiscoveryEvent['type']> = new Set([
  'acquisition_ui',
  'client_error',
]);

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

function capEntries(entries: readonly OutboxEntry[], max: number): OutboxEntry[] {
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
