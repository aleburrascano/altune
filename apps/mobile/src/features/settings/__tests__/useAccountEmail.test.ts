import type { Session } from '@supabase/supabase-js';

import { accountEmail } from '../hooks/useAccountEmail';

jest.mock('@shared/auth/useSession', () => ({}));

describe('accountEmail', () => {
  const signedIn = (email: string | undefined) => ({
    status: 'signed-in' as const,
    session: { user: { email } } as Session,
  });

  it('is the signed-in user email', () => {
    expect(accountEmail(signedIn('me@example.com'))).toBe('me@example.com');
  });

  it('is empty when the user has no email or nobody is signed in', () => {
    expect(accountEmail(signedIn(undefined))).toBe('');
    expect(accountEmail({ status: 'loading' })).toBe('');
    expect(accountEmail({ status: 'signed-out' })).toBe('');
  });
});
