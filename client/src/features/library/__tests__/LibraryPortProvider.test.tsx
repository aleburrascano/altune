import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import * as tracksApi from '@shared/api-client/tracks';

import { httpLibraryPort } from '@features/library/adapters/http/httpLibraryPort';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider, useLibraryPort } from '@features/library/ports/LibraryPortProvider';

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

describe('httpLibraryPort delegation', () => {
  afterEach(() => jest.restoreAllMocks());

  it('reaches a spy installed on the api-client after the port was imported', async () => {
    const page = { items: [], total: 0, limit: 1, offset: 0, has_more: false };
    jest.spyOn(tracksApi, 'getTracks').mockResolvedValue(page);

    await expect(httpLibraryPort.getTracks({ limit: 1, offset: 0 })).resolves.toBe(page);
  });
});
