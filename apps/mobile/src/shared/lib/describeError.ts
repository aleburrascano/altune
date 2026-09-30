import { isServerFaultStatus, type ContractError } from '@shared/errors';

import { isNetworkError } from './isNetworkError';

export interface ErrorCopy {
  readonly title: string;
  readonly body: string;
}

export const RETRY_TAIL = 'Please try again.';

function isServerError(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const status = (err as Error & { status?: unknown }).status;
  return typeof status === 'number' && isServerFaultStatus(status);
}

const CONTRACT_ERROR_NAME: ContractError['name'] = 'ContractError';

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

export function describeError(err: unknown): ErrorCopy {
  if (isContractError(err)) return UPDATE_REQUIRED_COPY;
  if (isNetworkError(err)) return OFFLINE_COPY;
  if (isServerError(err)) return SERVER_FAULT_COPY;
  return UNCLASSIFIED_COPY;
}
