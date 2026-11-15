import type { AuthPort } from '@features/signIn/ports/authPort';

import { reportSignInFailure } from '@features/signIn/adapters/http/reportSignInFailure';
import { bootstrapTestAuth, isTestAuthEnabled } from '@features/signIn/adapters/http/testAuth';

export const httpAuthPort: AuthPort = {
  reportSignInFailure: (reason) => reportSignInFailure(reason),
  isTestAuthEnabled: () => isTestAuthEnabled(),
  bootstrapTestAuth: () => bootstrapTestAuth(),
};
