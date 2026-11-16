import type { AuthPort } from '@features/signIn';

type FailureReason = Parameters<AuthPort['reportSignInFailure']>[0];

export type MemoryAuthPort = AuthPort & { readonly reportedFailures: readonly FailureReason[] };

export function createMemoryAuthPort({ testAuthEnabled = false } = {}): MemoryAuthPort {
  const reportedFailures: FailureReason[] = [];
  return {
    reportedFailures,
    reportSignInFailure: (reason) => {
      reportedFailures.push(reason);
    },
    isTestAuthEnabled: () => testAuthEnabled,
    bootstrapTestAuth: () => Promise.resolve(testAuthEnabled),
  };
}
