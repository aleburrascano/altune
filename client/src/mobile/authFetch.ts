import { startDeadline } from '@shared/timing';
import type { Deadline } from '@shared/timing';

const AUTH_FETCH_TIMEOUT_MS = 10_000;

export type AuthFetch = (
  input: Parameters<typeof fetch>[0],
  init?: RequestInit,
) => Promise<Response>;

function releaseWhenBodyDrains(response: Response, release: () => void): void {
  if (response.body == null) {
    release();
    return;
  }
  response.clone().arrayBuffer().then(release, release);
}

type FetchInput = Parameters<AuthFetch>[0];

function releaseOnSettle(deadline: Deadline, response: Promise<Response>): Promise<Response> {
  function failed(error: unknown): never {
    deadline.release();
    throw error;
  }
  return response.then((answered) => {
    releaseWhenBodyDrains(answered, deadline.release);
    return answered;
  }, failed);
}

function sendWithinDeadline(send: AuthFetch, input: FetchInput, init?: RequestInit) {
  const deadline = startDeadline(init?.signal ?? undefined, AUTH_FETCH_TIMEOUT_MS);
  return releaseOnSettle(deadline, send(input, { ...init, signal: deadline.signal }));
}

export function fetchWithinAuthDeadline(send: AuthFetch): AuthFetch {
  return (input, init) => sendWithinDeadline(send, input, init);
}
