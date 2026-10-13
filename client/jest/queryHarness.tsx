import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { AcquisitionPortProvider } from '@shared/acquisition/ports/AcquisitionPortProvider';
import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';
import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';
import { PlaybackContext } from '@shared/playback/usePlayback';

import { DetailPortProvider } from '@features/detail/ports/DetailPortProvider';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

interface WrapperProps {
  children: React.ReactNode;
}

export const idlePlayback: PlaybackContextValue = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
  errorKind: null,
  play: async () => {},
  startQueue: async () => {},
  skipToQueueIndex: async () => {},
  reorderUpcoming: async () => {},
  appendToQueue: async () => {},
  insertNext: async () => {},
  skipNext: async () => {},
  skipPrevious: async () => {},
  removeQueueIndex: async () => {},
  pause: () => {},
  resume: () => {},
  seekTo: () => {},
  setRate: () => {},
  stop: () => {},
  retry: () => {},
};

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
