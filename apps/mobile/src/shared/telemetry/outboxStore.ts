import { Directory, File, Paths } from 'expo-file-system';

import type { OutboxEntry } from './outbox';

const OUTBOX_DIR = 'telemetry';
const OUTBOX_FILE = 'critical-outbox.json';

function outboxFile(): File {
  const dir = new Directory(Paths.document, OUTBOX_DIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return new File(dir, OUTBOX_FILE);
}

export function loadPersistedOutbox(): OutboxEntry[] {
  try {
    const file = outboxFile();
    if (!file.exists) return [];
    const parsed: unknown = JSON.parse(file.textSync());
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((e): e is OutboxEntry => {
      if (typeof e !== 'object' || e === null) return false;
      const eventId = (e as OutboxEntry).event_id;
      return typeof eventId === 'string' && eventId.length > 0;
    });
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
  } catch {}
}
