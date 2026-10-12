import type { AuthPort } from '@features/auth/ports/authPort';

import { reportSignInFailure } from '@features/auth/adapters/http/reportSignInFailure';
import { bootstrapTestAuth, isTestAuthEnabled } from '@features/auth/adapters/http/testAuth';

export const httpAuthPort: AuthPort = {
  reportSignInFailure: (reason) => reportSignInFailure(reason),
  isTestAuthEnabled: () => isTestAuthEnabled(),
  bootstrapTestAuth: () => bootstrapTestAuth(),
};
