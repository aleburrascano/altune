import { createTransientRetryOptions } from '@shared/query';

const MIDDLE_OF_THE_JITTER = 0.5;

export function retryOptionsWithJitter(sample = MIDDLE_OF_THE_JITTER) {
  return createTransientRetryOptions({ random: () => sample });
}
