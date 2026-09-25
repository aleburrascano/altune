import type { NetworkError } from '@shared/errors';

/**
 * The `name` every api-client `NetworkError` carries. Read structurally, the way
 * `isAbort` reads `AbortError`: `shared/lib`'s purity invariant forbids a runtime
 * import of `@shared/api-client`, so `instanceof NetworkError` is not available
 * here. The type-only import keeps the contract linked to the class.
 */
const NETWORK_ERROR_NAME: NetworkError['name'] = 'NetworkError';

/** Transport wording from throws the api-client never wrapped (RN fetch, auth SDK). */
const UNWRAPPED_TRANSPORT_MESSAGE = /network|fetch|timeout|connection/i;

export function isNetworkError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  if (err.name === NETWORK_ERROR_NAME) return true;
  return UNWRAPPED_TRANSPORT_MESSAGE.test(err.message);
}
