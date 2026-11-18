import { installAuthClient } from '@shared/auth';
import { authClientOver } from '@testing/fakeAuthClient';

export type SessionLookup = jest.Mock<Promise<unknown>, []>;

export function installSessionLookup(): SessionLookup {
  const getSession: SessionLookup = jest.fn();
  installAuthClient(authClientOver({ getSession }));
  return getSession;
}

export function signedInWith(accessToken = 'tok'): SessionLookup {
  const getSession = installSessionLookup();
  getSession.mockResolvedValue({ data: { session: { access_token: accessToken } }, error: null });
  return getSession;
}
