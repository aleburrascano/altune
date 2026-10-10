import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createMemoryDetailPort } from '@features/detail/__tests__/memoryDetailPort';
import { DetailPortProvider, useDetailPort } from '@features/detail/ports/DetailPortProvider';

describe('useDetailPort', () => {
  it('throws when no provider is mounted', () => {
    jest.spyOn(console, 'error').mockImplementation(() => {});

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
