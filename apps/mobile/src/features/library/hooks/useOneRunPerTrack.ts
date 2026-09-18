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

/** A track mutation whose in-flight state is a question about a track id. */
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

// Only this module's hooks mutate under these keys, and every one of them takes a TrackId.
const trackIdOf = (run: Mutation<unknown, Error, unknown, unknown>): TrackId =>
  run.state.variables as TrackId;

function hasPendingRun(
  cache: MutationCache,
  mutationKey: TrackMutationKey,
  trackId: TrackId,
): boolean {
  return cache.findAll(pendingRunsOf(mutationKey)).some((run) => trackIdOf(run) === trackId);
}

/**
 * Holds every row of a list to one in-flight run per track id. A whole list shares one
 * `useMutation`, and that observer remembers only the latest `.mutate()` call, so a second
 * row's tap used to clear the first row's indicator and re-arm its button mid-flight (#1702).
 * The mutation cache keeps one entry per call, so both answers come from there: the
 * indicator by subscription, the guard by a synchronous read, because a tap can land
 * before React has re-rendered the row that would have hidden the button.
 */
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
