import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

export function createTestQueryClient({ mutations = false }: { mutations?: boolean } = {}) {
  return new QueryClient({
    defaultOptions: mutations
      ? { queries: { retry: false }, mutations: { retry: false } }
      : { queries: { retry: false } },
  });
}

export function createWrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

export function mockSupabaseSession() {
  return {
    data: { session: { access_token: 'tok' } },
    error: null,
  };
}
