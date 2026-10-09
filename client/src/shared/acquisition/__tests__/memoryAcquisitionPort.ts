import type { TrackId } from '@shared/api-client/ids';
import { ApiError } from '@shared/failure/errors';

import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

const NOT_FOUND_STATUS = 404;

export interface MemoryAcquisitionSeed {
  trackIds?: readonly string[];
}

export interface MemoryAcquisitionPort {
  port: AcquisitionPort;
  readonly retried: readonly TrackId[];
}

export function createMemoryAcquisitionPort({
  trackIds = [],
}: MemoryAcquisitionSeed = {}): MemoryAcquisitionPort {
  const seeded = new Set<string>(trackIds);
  const retried: TrackId[] = [];
  return {
    port: {
      retryAcquisition: async (trackId) => {
        await Promise.resolve();
        if (!seeded.has(trackId)) throw new ApiError(NOT_FOUND_STATUS, 'track not found');
        retried.push(trackId);
      },
    },
    retried,
  };
}
