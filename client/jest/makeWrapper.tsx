import React from 'react';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { FeaturePorts } from '@/wiring/FeaturePorts';
import { httpFeaturePorts } from '@/wiring/httpFeaturePorts';

export function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
      </QueryClientProvider>
    );
  };
}
