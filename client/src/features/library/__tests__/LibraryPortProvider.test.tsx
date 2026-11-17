import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createMemoryLibraryPort, LibraryPortProvider, useLibraryPort } from '@features/library';

describe('useLibraryPort', () => {
  it('throws when no provider is mounted', () => {
    jest.spyOn(console, 'error').mockImplementation(() => {});

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
