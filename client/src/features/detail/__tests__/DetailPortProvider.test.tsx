import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { DetailPortProvider, useDetailPort } from '@features/detail';
import { createMemoryDetailPort } from '@testing/fakes/detail/inMemoryDetailPort';
describe('useDetailPort', () => {
  it('throws when no provider is mounted', () => {
    expect(() => renderHook(() => useDetailPort())).toThrow('no detail port provided');
  });

  it('hands back the port the provider was given', () => {
    const port = createMemoryDetailPort();
    function wrapper({ children }: { children: ReactNode }) {
      return <DetailPortProvider port={port}>{children}</DetailPortProvider>;
    }

    const { result } = renderHook(() => useDetailPort(), { wrapper });

    expect(result.current).toBe(port);
  });
});
