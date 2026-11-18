import type * as apiClient from '@shared/apiClient';

export interface AcquisitionPort {
  retryAcquisition: typeof apiClient.retryAcquisition;
}
