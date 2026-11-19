import type { AuthPort } from '@features/signIn/ports/authPort';

import { reportSignInFailure } from '@features/signIn/adapters/http/reportSignInFailure';

export const httpAuthPort: AuthPort = {
  reportSignInFailure: (reason) => reportSignInFailure(reason),
};
