import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { SignInProvider, useAuthPort } from '@features/signIn/view/SignInProvider';
import type { AuthBrowser, AuthPort } from '@features/signIn/ports/ports';
import { intents, recovery } from '@features/signIn/__tests__/authTestUtils';
import { createSignInHealth } from '@features/signIn/core/signInHealth';
import { createAttemptLockoutStore } from '@features/signIn/core/attemptLockoutStore';

const port: AuthPort = {
  reportSignInFailure: () => undefined,
};

const browser: AuthBrowser = {
  openAuthSession: () => Promise.resolve({ type: 'cancel' }),
  dismissAuthSession: () => undefined,
};

interface WrapperProps {
  children: ReactNode;
}

describe('SignInProvider', () => {
  it('provides the port to useAuthPort', () => {
    function wrapper({ children }: WrapperProps) {
      return (
        <SignInProvider
          port={port}
          browser={browser}
          lockout={createAttemptLockoutStore()}
          recovery={recovery}
          intents={intents}
          health={createSignInHealth()}
        >
          {children}
        </SignInProvider>
      );
    }
    const { result } = renderHook(() => useAuthPort(), { wrapper });
    expect(result.current).toBe(port);
  });

  it('throws when no provider is mounted', () => {
    expect(() => renderHook(() => useAuthPort())).toThrow(
      'no auth port provided: mount SignInProvider',
    );
  });
});
