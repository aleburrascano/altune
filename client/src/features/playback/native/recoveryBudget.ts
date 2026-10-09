import type { TrackKey } from '@shared/playback/trackKey';

export const RECOVERY_ATTEMPTS_PER_TRACK = 2;

export const RECOVERY_COOLDOWN_BASE_MS = 30_000;

const RECOVERY_MEMORY_MS = 10 * 60_000;

export const MAX_TRACKED_RECOVERIES = 64;

type RecoveryRun = { attempts: number; lastAttemptAt: number };

const recoveryRuns = new Map<TrackKey, RecoveryRun>();

function hasSettled(run: RecoveryRun, now: number): boolean {
  const elapsed = now - run.lastAttemptAt;
  return elapsed < 0 || elapsed >= RECOVERY_MEMORY_MS;
}

function cooldownMs(attempts: number): number {
  if (attempts < RECOVERY_ATTEMPTS_PER_TRACK) return 0;
  const doubledPerExtraAttempt =
    RECOVERY_COOLDOWN_BASE_MS * 2 ** (attempts - RECOVERY_ATTEMPTS_PER_TRACK);
  return Math.min(doubledPerExtraAttempt, RECOVERY_MEMORY_MS);
}

function isCoolingDown(run: RecoveryRun, now: number): boolean {
  return now - run.lastAttemptAt < cooldownMs(run.attempts);
}

function forgetSettledRuns(now: number): void {
  for (const [key, run] of recoveryRuns) {
    if (hasSettled(run, now)) recoveryRuns.delete(key);
  }
}

export function claimRecoveryAttempt(key: TrackKey, now: number): boolean {
  forgetSettledRuns(now);
  const run = recoveryRuns.get(key);
  if (run !== undefined && isCoolingDown(run, now)) return false;
  if (run === undefined && recoveryRuns.size >= MAX_TRACKED_RECOVERIES) return false;
  recoveryRuns.set(key, { attempts: (run?.attempts ?? 0) + 1, lastAttemptAt: now });
  return true;
}

export function resetRecoveryBudget(): void {
  recoveryRuns.clear();
}
