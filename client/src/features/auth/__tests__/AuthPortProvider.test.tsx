import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { AuthPortProvider, useAuthPort } from '@features/auth/ports/AuthPortProvider';
import type { AuthPort } from '@features/auth/ports/authPort';

const port: AuthPort = {
  reportSignInFailure: () => undefined,
  isTestAuthEnabled: () => false,
  bootstrapTestAuth: () => Promise.resolve(false),
};

interface WrapperProps {
  children: ReactNode;
}

describe('AuthPortProvider', () => {
  it('provides the port to useAuthPort', () => {
    function wrapper({ children }: WrapperProps) {
      return <AuthPortProvider port={port}>{children}</AuthPortProvider>;
    }
    const { result } = renderHook(() => useAuthPort(), { wrapper });
    expect(result.current).toBe(port);
  });

  it('throws when no provider is mounted', () => {
    const spy = jest.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(() => renderHook(() => useAuthPort())).toThrow(
      'no auth port provided: mount AuthPortProvider',
    );
    spy.mockRestore();
  });
});
