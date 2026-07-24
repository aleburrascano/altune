/**
 * Disk persistence for the critical-event outbox.
 *
 * The outbox was in-memory only, so a hard app-kill while offline lost every
 * queued label — precisely the events it exists to protect. The documented
 * blocker was "needs a durable client store (AsyncStorage / SQLite), which is
 * not a current dependency". `expo-file-system` already is one (the audio
 * prefetch cache uses it), so durability costs no new native module and no
 * ADR — a small JSON file in the app's document directory is enough for a queue
 * capped at 50 entries.
 *
 * Every operation is best-effort and synchronous-on-disk: telemetry must never
 * throw into a caller, and a corrupt or missing file simply means an empty
 * queue. Writes are small and infrequent (one per critical event, one per
 * successful flush), so the cost does not warrant batching.
 */
import { Directory, File, Paths } from 'expo-file-system';

import type { OutboxEntry } from './outbox';

const OUTBOX_DIR = 'telemetry';
const OUTBOX_FILE = 'critical-outbox.json';

function outboxFile(): File {
  const dir = new Directory(Paths.document, OUTBOX_DIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return new File(dir, OUTBOX_FILE);
}

/** Entries left over from a previous run. Empty on anything unexpected —
 *  a lost queue is bad, but a crash loop on boot is worse. */
export function loadPersistedOutbox(): OutboxEntry[] {
  try {
    const file = outboxFile();
    if (!file.exists) return [];
    const parsed: unknown = JSON.parse(file.textSync());
    if (!Array.isArray(parsed)) return [];
    // Entries are only trusted far enough to be re-POSTed; the server validates
    // and dedups on event_id regardless.
    return parsed.filter(
      (e): e is OutboxEntry =>
        typeof e === 'object' && e !== null && typeof (e as OutboxEntry).event_id === 'string',
    );
  } catch {
    return [];
  }
}

export function persistOutbox(entries: readonly OutboxEntry[]): void {
  try {
    const file = outboxFile();
    if (entries.length === 0) {
      if (file.exists) file.delete();
      return;
    }
    file.write(JSON.stringify(entries));
  } catch {
    // Disk full / sandbox denial — the in-memory queue still flushes this
    // session; only cross-kill durability is lost.
  }
}
