import type { NetworkError } from '@shared/errors';

const NETWORK_ERROR_NAME: NetworkError['name'] = 'NetworkError';

const UNWRAPPED_TRANSPORT_MESSAGE = /network|fetch|timeout|connection/i;

export function isNetworkError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  if (err.name === NETWORK_ERROR_NAME) return true;
  return UNWRAPPED_TRANSPORT_MESSAGE.test(err.message);
}
