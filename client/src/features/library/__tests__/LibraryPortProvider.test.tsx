import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { LibraryPortProvider, useLibraryPort } from '@features/library';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';

describe('useLibraryPort', () => {
  it('throws when no provider is mounted', () => {
    expect(() => renderHook(() => useLibraryPort())).toThrow('no library port provided');
  });

  it('hands back the port the provider was given', () => {
    const port = createMemoryLibraryPort();
    function wrapper({ children }: { children: ReactNode }) {
      return <LibraryPortProvider port={port}>{children}</LibraryPortProvider>;
    }

    const { result } = renderHook(() => useLibraryPort(), { wrapper });

    expect(result.current).toBe(port);
  });

  it('hands back the innermost port when providers are nested', () => {
    const outer = createMemoryLibraryPort();
    const inner = createMemoryLibraryPort();
    function wrapper({ children }: { children: ReactNode }) {
      return (
        <LibraryPortProvider port={outer}>
          <LibraryPortProvider port={inner}>{children}</LibraryPortProvider>
        </LibraryPortProvider>
      );
    }

    const { result } = renderHook(() => useLibraryPort(), { wrapper });

    expect(result.current).toBe(inner);
  });
});
