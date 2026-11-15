import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { AcquisitionPortProvider } from '@shared/acquisition';
import type { AcquisitionPort } from '@shared/acquisition';
import type { PlaybackContextValue } from '@shared/playback';
import { makeIdlePlayback } from '@testing/factories';
import { PlaybackContext } from '@shared/playback';

import { DetailPortProvider } from '@features/detail';
import type { DetailPort } from '@features/detail';
import { FeaturePorts, httpFeaturePorts } from '@/mobile';

interface WrapperProps {
  children: React.ReactNode;
}

export const idlePlayback = makeIdlePlayback();

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

export function createPlaybackWrapper(
  client: QueryClient,
  playback: PlaybackContextValue = idlePlayback,
) {
  const Base = createWrapper(client);
  return function Wrapper({ children }: WrapperProps) {
    return (
      <PlaybackContext.Provider value={playback}>
        <Base>{children}</Base>
      </PlaybackContext.Provider>
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

export { signInTestUser } from '@testing/signInTestUser';
