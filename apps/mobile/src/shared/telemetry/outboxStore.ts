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
  } catch {}
}
