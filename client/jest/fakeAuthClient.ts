import type { AuthClient } from '@shared/auth';

type Auth = AuthClient['auth'];

export type LooseAuth = { [K in keyof Auth]?: unknown };

export function authClientOver(auth: LooseAuth): AuthClient {
  return { auth: auth as Auth, clearPersistedSession: () => Promise.resolve() };
}
