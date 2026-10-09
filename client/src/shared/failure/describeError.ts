import {
  ApiError,
  isServerFaultStatus,
  type ContractError,
  isNetworkError,
} from '@shared/failure/errors';

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

const RATE_LIMITED_COPY: ErrorCopy = {
  title: 'Too many requests',
  body: 'You are doing that too often. Wait a moment, then try again.',
};
const REJECTED_COPY: ErrorCopy = {
  title: 'Something went wrong',
  body: 'We could not complete that request.',
};

const HTTP_TOO_MANY_REQUESTS = 429;

export function describeError(err: unknown): ErrorCopy {
  if (isContractError(err)) return UPDATE_REQUIRED_COPY;
  if (isNetworkError(err)) return OFFLINE_COPY;
  if (isServerError(err)) return SERVER_FAULT_COPY;
  if (err instanceof ApiError) {
    return err.status === HTTP_TOO_MANY_REQUESTS ? RATE_LIMITED_COPY : REJECTED_COPY;
  }
  return UNCLASSIFIED_COPY;
}
