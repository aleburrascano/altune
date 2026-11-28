import type { createTransientRetryOptions } from '@shared/query';

export type RetryOptions = ReturnType<typeof createTransientRetryOptions>;
