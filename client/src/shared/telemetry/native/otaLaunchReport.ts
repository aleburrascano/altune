import * as Updates from 'expo-updates';

import { recordUserAction } from '@shared/telemetry/userTelemetry';

const DAY_MS = 86_400_000;
const LOG_WINDOW_DAYS = 7;
const LOG_WINDOW_MS = LOG_WINDOW_DAYS * DAY_MS;
const MAX_REPORTED_ERRORS = 5;
const FAILED_LEVELS: ReadonlySet<string> = new Set(['error', 'fatal']);

type ReportLine = (line: string) => void;

function recordLaunchFailure(line: string): void {
  recordUserAction({ action: 'ota.launch', outcome: 'failed', error: line });
}

async function failedEntries() {
  const entries = await Updates.readLogEntriesAsync(LOG_WINDOW_MS);
  return entries.filter((e) => FAILED_LEVELS.has(e.level)).slice(-MAX_REPORTED_ERRORS);
}

function emergencyLines(): string[] {
  if (!Updates.isEmergencyLaunch) return [];
  return [`emergency launch: ${Updates.emergencyLaunchReason ?? 'no reason'}`];
}

function entryLine(e: Updates.UpdatesLogEntry): string {
  return `${e.code} ${e.updateId ?? '-'}: ${e.message}`;
}

export async function reportOtaLaunchFailures(
  report: ReportLine = recordLaunchFailure,
): Promise<void> {
  if (!Updates.isEnabled) return;
  const failures = await failedEntries();
  [...emergencyLines(), ...failures.map(entryLine)].forEach((line) => report(line));
  if (failures.length > 0) await Updates.clearLogEntriesAsync();
}
