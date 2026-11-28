export const trackMutationKeys = {
  retryAcquisition: ['track', 'retry-acquisition'] as const,
  reacquire: ['track', 'reacquire'] as const,
};

export type TrackMutationKey = (typeof trackMutationKeys)[keyof typeof trackMutationKeys];
