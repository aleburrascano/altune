import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { AcquisitionPortProvider } from '@shared/acquisition/ports/AcquisitionPortProvider';
import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

import { DetailPortProvider } from '@features/detail/ports/DetailPortProvider';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

export function createTestQueryClient({ mutations = false }: { mutations?: boolean } = {}) {
  return new QueryClient({
    defaultOptions: mutations
      ? { queries: { retry: false }, mutations: { retry: false } }
      : { queries: { retry: false } },
  });
}

export function createWrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
      </QueryClientProvider>
    );
  };
}

export function createPortWrapper(
  client: QueryClient,
  ports: { detail: DetailPort; acquisition?: AcquisitionPort },
) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    const detail = <DetailPortProvider port={ports.detail}>{children}</DetailPortProvider>;
    return (
      <QueryClientProvider client={client}>
        {ports.acquisition === undefined ? (
          detail
        ) : (
          <AcquisitionPortProvider port={ports.acquisition}>{detail}</AcquisitionPortProvider>
        )}
      </QueryClientProvider>
    );
  };
}

export function mockSupabaseSession() {
  return {
    data: { session: { access_token: 'tok' } },
    error: null,
  };
}
