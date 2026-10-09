import React from 'react';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';

export function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}
