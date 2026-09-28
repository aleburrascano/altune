import { useMemo } from 'react';
import {
  useMutationState,
  useQueryClient,
  type Mutation,
  type MutationCache,
  type MutationFilters,
  type UseMutationResult,
} from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';

export const trackMutationKeys = {
  retryAcquisition: ['track', 'retry-acquisition'] as const,
  reacquire: ['track', 'reacquire'] as const,
};

type TrackMutationKey = (typeof trackMutationKeys)[keyof typeof trackMutationKeys];

export type TrackMutation<TData, TContext> = Omit<
  UseMutationResult<TData, Error, TrackId, TContext>,
  'mutate' | 'mutateAsync'
> & {
  mutate: (trackId: TrackId) => void;
  isInFlight: (trackId: TrackId) => boolean;
};

const pendingRunsOf = (mutationKey: TrackMutationKey): MutationFilters => ({
  mutationKey,
  status: 'pending',
});

const trackIdOf = (run: Mutation<unknown, Error, unknown, unknown>): TrackId =>
  run.state.variables as TrackId;

function hasPendingRun(
  cache: MutationCache,
  mutationKey: TrackMutationKey,
  trackId: TrackId,
): boolean {
  return cache.findAll(pendingRunsOf(mutationKey)).some((run) => trackIdOf(run) === trackId);
}

export function useOneRunPerTrack<TData, TContext>(
  mutation: UseMutationResult<TData, Error, TrackId, TContext>,
  mutationKey: TrackMutationKey,
): TrackMutation<TData, TContext> {
  const mutationCache = useQueryClient().getMutationCache();
  const pendingIds = useMutationState({ filters: pendingRunsOf(mutationKey), select: trackIdOf });
  const inFlight = useMemo(() => new Set<TrackId>(pendingIds), [pendingIds]);
  const { mutate, ...run } = mutation;

  return {
    ...run,
    mutate: (trackId: TrackId) => {
      if (hasPendingRun(mutationCache, mutationKey, trackId)) return;
      mutate(trackId);
    },
    isInFlight: (trackId: TrackId) => inFlight.has(trackId),
  };
}
