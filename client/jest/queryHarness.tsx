import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { AcquisitionPortProvider } from '@shared/acquisition';
import type { AcquisitionPort } from '@shared/acquisition';
import type { PlaybackContextValue } from '@shared/playback';
import { makeIdlePlayback } from '@testing/factories';
import { PlaybackContext } from '@shared/playback';

import { DetailPortProvider, recordWarnings } from '@features/detail';
import type { DetailPort, RecordingDetailPort } from '@features/detail';
import { FeaturePorts, httpFeaturePorts, type FeaturePortSet } from '@/mobile';

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

export type RecordingFeaturePorts = FeaturePortSet & { detail: RecordingDetailPort };

export function createRecordingPorts(): RecordingFeaturePorts {
  return { ...httpFeaturePorts, detail: recordWarnings(httpFeaturePorts.detail) };
}

export function createWrapper(client: QueryClient, ports: FeaturePortSet = httpFeaturePorts) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <FeaturePorts ports={ports}>{children}</FeaturePorts>
      </QueryClientProvider>
    );
  };
}

export function createPlaybackWrapper(
  client: QueryClient,
  playback: PlaybackContextValue = idlePlayback,
  ports: FeaturePortSet = httpFeaturePorts,
) {
  const Base = createWrapper(client, ports);
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
        <AcquisitionPortProvider port={ports.acquisition ?? httpFeaturePorts.acquisition}>
          {detail}
        </AcquisitionPortProvider>
      </QueryClientProvider>
    );
  };
}

export { signInTestUser } from '@testing/signInTestUser';
