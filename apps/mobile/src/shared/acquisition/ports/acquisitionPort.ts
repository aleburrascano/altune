import type * as tracks from '@shared/api-client/tracks';

export interface AcquisitionPort {
  retryAcquisition: typeof tracks.retryAcquisition;
}
