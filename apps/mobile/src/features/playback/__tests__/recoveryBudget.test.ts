import type { TrackKey } from '@shared/playback/trackKey';

import {
  MAX_TRACKED_RECOVERIES,
  RECOVERY_ATTEMPTS_PER_TRACK,
  RECOVERY_COOLDOWN_BASE_MS,
  claimRecoveryAttempt,
  resetRecoveryBudget,
} from '../native/recoveryBudget';

const KEY = 'library:trk-1' as TrackKey;
const OTHER_KEY = 'library:trk-2' as TrackKey;
const START = Date.parse('2026-09-19T10:00:00Z');
const MEMORY_WINDOW_MS = 10 * 60_000;

function spendBudget(key: TrackKey, now: number): void {
  for (let i = 0; i < RECOVERY_ATTEMPTS_PER_TRACK; i += 1) claimRecoveryAttempt(key, now);
}

describe('claimRecoveryAttempt', () => {
  afterEach(resetRecoveryBudget);

  it('grants the per-track attempts back to back, then refuses', () => {
    const grants = Array.from({ length: RECOVERY_ATTEMPTS_PER_TRACK + 3 }, () =>
      claimRecoveryAttempt(KEY, START),
    );

    expect(grants.filter(Boolean)).toHaveLength(RECOVERY_ATTEMPTS_PER_TRACK);
  });

  it('keeps refusing until the base cooldown has passed, then grants one more', () => {
    spendBudget(KEY, START);

    expect(claimRecoveryAttempt(KEY, START + RECOVERY_COOLDOWN_BASE_MS - 1)).toBe(false);
    expect(claimRecoveryAttempt(KEY, START + RECOVERY_COOLDOWN_BASE_MS)).toBe(true);
  });

  it('doubles the cooldown for each attempt past the cap', () => {
    spendBudget(KEY, START);
    const third = START + RECOVERY_COOLDOWN_BASE_MS;
    claimRecoveryAttempt(KEY, third);

    expect(claimRecoveryAttempt(KEY, third + RECOVERY_COOLDOWN_BASE_MS)).toBe(false);
    expect(claimRecoveryAttempt(KEY, third + 2 * RECOVERY_COOLDOWN_BASE_MS)).toBe(true);
  });

  it('holds a separate budget per track', () => {
    spendBudget(KEY, START);

    expect(claimRecoveryAttempt(OTHER_KEY, START)).toBe(true);
  });

  it('forgets a track once the memory window has passed, restoring its budget', () => {
    spendBudget(KEY, START);

    const grants = Array.from({ length: RECOVERY_ATTEMPTS_PER_TRACK }, () =>
      claimRecoveryAttempt(KEY, START + MEMORY_WINDOW_MS),
    );

    expect(grants.every(Boolean)).toBe(true);
  });

  it('refuses a new key once it tracks the maximum number of failing tracks', () => {
    for (let i = 0; i < MAX_TRACKED_RECOVERIES; i += 1) {
      claimRecoveryAttempt(`library:f${i}` as TrackKey, START);
    }

    expect(claimRecoveryAttempt(OTHER_KEY, START)).toBe(false);
  });

  it('still serves a tracked key when the table is full', () => {
    claimRecoveryAttempt(KEY, START);
    for (let i = 1; i < MAX_TRACKED_RECOVERIES; i += 1) {
      claimRecoveryAttempt(`library:f${i}` as TrackKey, START);
    }

    expect(claimRecoveryAttempt(KEY, START)).toBe(true);
  });

  it('starts every track fresh after a reset', () => {
    spendBudget(KEY, START);

    resetRecoveryBudget();

    expect(claimRecoveryAttempt(KEY, START)).toBe(true);
  });
});
