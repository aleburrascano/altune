import type * as tracks from '@shared/apiClient/tracks';

export interface AcquisitionPort {
  retryAcquisition: typeof tracks.retryAcquisition;
}
