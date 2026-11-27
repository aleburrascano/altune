import { warn } from '@shared/log';
import { createHealthTally } from '@shared/telemetry';

const HEALTH_KEYS = ['signin_ok'] as const;

type HealthKey = (typeof HEALTH_KEYS)[number];

const health = createHealthTally<HealthKey>({
  eventType: 'playback_health',
  keys: HEALTH_KEYS,
  onFlushFailure: (error: unknown) => {
    warn('[auth] sign-in health flush failed', { error });
  },
});

export function recordSignInOk(): void {
  health.count('signin_ok');
}

export const flushSignInHealth = health.flush;

export const _resetSignInHealthForTest = health.reset;
