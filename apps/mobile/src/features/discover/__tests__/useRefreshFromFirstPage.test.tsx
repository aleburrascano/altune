import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { useRefreshFromFirstPage } from '../hooks/useRefreshFromFirstPage';

const queryKey = ['discovery', 'search', 'q', true];

function pagesOf(title: string) {
  return { pages: [{ results: [{ title }] }], pageParams: [{ offset: 0 }] };
}

describe('useRefreshFromFirstPage', () => {
  it('leaves no held slate when the refetch rejects', async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(queryKey, pagesOf('old-row'));
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const refetch = jest.fn().mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useRefreshFromFirstPage(queryKey, refetch), { wrapper });

    await act(async () => {
      await result.current.refresh().catch(() => undefined);
    });

    expect(result.current.held).toBeUndefined();
  });
});
