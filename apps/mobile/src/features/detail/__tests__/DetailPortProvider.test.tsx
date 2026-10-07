import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import { createMemoryDetailPort } from '@features/detail/__tests__/memoryDetailPort';
import { DetailPortProvider, useDetailPort } from '@features/detail/ports/DetailPortProvider';

describe('useDetailPort', () => {
  it('hands back the HTTP port when no provider is mounted', () => {
    const { result } = renderHook(() => useDetailPort());

    expect(result.current).toBe(httpDetailPort);
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
