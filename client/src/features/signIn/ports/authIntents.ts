import type { AuthIntentResult } from '@features/signIn/core/authIntent';
import type { AuthLinkIntent } from '@features/signIn/core/parseAuthLink';

export type AuthRouter = { replace(href: '/library' | '/reset-password'): void };

export interface AuthIntents {
  complete(intent: AuthLinkIntent, router: AuthRouter): Promise<AuthIntentResult>;
}
