import type { ContractError } from '@shared/api-client/errors';

import { isNetworkError } from './isNetworkError';

export interface ErrorCopy {
  readonly title: string;
  readonly body: string;
}

/**
 * The shared "…try again" tail. Rendered error bodies and the mutation Alerts
 * compose their own verb sentence in front of it so the retry ask reads the
 * same everywhere.
 */
export const RETRY_TAIL = 'Please try again.';

/**
 * A 5xx from the API. Detected structurally by the `status` field `ApiError`
 * carries (see `@shared/api-client/errors`): this module lives in `shared/lib`,
 * whose purity invariant forbids a runtime import of `@shared/api-client`, so it
 * reads the same contract without pulling the class in.
 */
function isServerError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const status = (err as Error & { status?: unknown }).status;
  return typeof status === 'number' && status >= 500;
}

/** The `name` every api-client `ContractError` carries, read the way `isNetworkError` reads its. */
const CONTRACT_ERROR_NAME: ContractError['name'] = 'ContractError';

/**
 * A payload this build cannot decode. Permanent, not transient: the wire shape
 * has moved past what this install knows, so only a newer build reaches it.
 */
function isContractError(err: unknown): boolean {
  return err instanceof Error && err.name === CONTRACT_ERROR_NAME;
}

const UPDATE_REQUIRED_COPY: ErrorCopy = {
  title: 'Update required',
  body: 'This version of Altune is out of date. Update the app to continue.',
};
const OFFLINE_COPY: ErrorCopy = {
  title: 'No connection',
  body: 'Check your connection and try again.',
};
const SERVER_FAULT_COPY: ErrorCopy = {
  title: 'Something went wrong',
  body: `Something went wrong on our end. ${RETRY_TAIL}`,
};
const UNCLASSIFIED_COPY: ErrorCopy = {
  title: 'Something went wrong',
  body: `Something went wrong. ${RETRY_TAIL}`,
};

/**
 * The one mapper from an unknown thrown value to user-facing `{ title, body }`,
 * so a screen never hand-writes the branch again.
 *
 * The permanent failure is asked first: `isNetworkError` falls back to transport
 * *wording*, which a contract detail can quote (a rejected enum listing `timeout`
 * among its allowed values), and offering "try again" for a shape this build can
 * never read is a prompt no user can act on.
 */
export function describeError(err: unknown): ErrorCopy {
  if (isContractError(err)) return UPDATE_REQUIRED_COPY;
  if (isNetworkError(err)) return OFFLINE_COPY;
  if (isServerError(err)) return SERVER_FAULT_COPY;
  return UNCLASSIFIED_COPY;
}
