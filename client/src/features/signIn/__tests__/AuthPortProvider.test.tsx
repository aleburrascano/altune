import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import {
  attemptLockout,
  authIntents,
  AuthPortProvider,
  recoveryUnlock,
  useAuthPort,
} from '@features/signIn';
import type { AuthBrowser, AuthPort } from '@features/signIn';

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

describe('AuthPortProvider', () => {
  it('provides the port to useAuthPort', () => {
    function wrapper({ children }: WrapperProps) {
      return (
        <AuthPortProvider
          port={port}
          browser={browser}
          lockout={attemptLockout}
          recovery={recoveryUnlock}
          intents={authIntents}
        >
          {children}
        </AuthPortProvider>
      );
    }
    const { result } = renderHook(() => useAuthPort(), { wrapper });
    expect(result.current).toBe(port);
  });

  it('throws when no provider is mounted', () => {
    expect(() => renderHook(() => useAuthPort())).toThrow(
      'no auth port provided: mount AuthPortProvider',
    );
  });
});
