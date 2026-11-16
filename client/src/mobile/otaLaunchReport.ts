import * as Updates from 'expo-updates';

import { recordUserAction } from '@shared/telemetry';

const DAY_MS = 86_400_000;
const LOG_WINDOW_DAYS = 7;
const LOG_WINDOW_MS = LOG_WINDOW_DAYS * DAY_MS;
const MAX_REPORTED_ERRORS = 5;
const FAILED_LEVELS: ReadonlySet<string> = new Set(['error', 'fatal']);

type ReportLine = (line: string) => void;

export type UpdateLog = Pick<
  typeof Updates,
  | 'isEnabled'
  | 'isEmergencyLaunch'
  | 'emergencyLaunchReason'
  | 'readLogEntriesAsync'
  | 'clearLogEntriesAsync'
>;

function recordLaunchFailure(line: string): void {
  recordUserAction({ action: 'ota.launch', outcome: 'failed', error: line });
}

async function failedEntries(updates: UpdateLog) {
  const entries = await updates.readLogEntriesAsync(LOG_WINDOW_MS);
  return entries.filter((e) => FAILED_LEVELS.has(e.level)).slice(-MAX_REPORTED_ERRORS);
}

function emergencyLines(updates: UpdateLog): string[] {
  if (!updates.isEmergencyLaunch) return [];
  return [`emergency launch: ${updates.emergencyLaunchReason ?? 'no reason'}`];
}

function entryLine(e: Updates.UpdatesLogEntry): string {
  return `${e.code} ${e.updateId ?? '-'}: ${e.message}`;
}

export async function reportOtaLaunchFailures(
  updates: UpdateLog = Updates,
  report: ReportLine = recordLaunchFailure,
): Promise<void> {
  if (!updates.isEnabled) return;
  const failures = await failedEntries(updates);
  [...emergencyLines(updates), ...failures.map(entryLine)].forEach((line) => report(line));
  if (failures.length > 0) await updates.clearLogEntriesAsync();
}
