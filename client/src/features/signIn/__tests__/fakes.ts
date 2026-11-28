import type { AuthPort } from '@features/signIn/ports/ports';

type FailureReason = Parameters<AuthPort['reportSignInFailure']>[0];

export type MemoryAuthPort = AuthPort & { readonly reportedFailures: readonly FailureReason[] };

export function createMemoryAuthPort(): MemoryAuthPort {
  const reportedFailures: FailureReason[] = [];
  return {
    reportedFailures,
    reportSignInFailure: (reason) => {
      reportedFailures.push(reason);
    },
  };
}
