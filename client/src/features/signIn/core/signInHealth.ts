import { warn } from '@shared/log';
import { createHealthTally } from '@shared/telemetry';

const HEALTH_KEYS = ['signin_ok'] as const;

type HealthKey = (typeof HEALTH_KEYS)[number];

export interface SignInHealth {
  recordSignInOk(): void;
  flush(): void;
}

export function createSignInHealth(): SignInHealth {
  const health = createHealthTally<HealthKey>({
    eventType: 'playback_health',
    keys: HEALTH_KEYS,
    onFlushFailure: (error: unknown) => {
      warn('[auth] sign-in health flush failed', { error });
    },
  });
  return { recordSignInOk: () => health.count('signin_ok'), flush: health.flush };
}
