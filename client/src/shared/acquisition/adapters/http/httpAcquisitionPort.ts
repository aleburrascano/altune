import { retryAcquisition } from '@shared/apiClient/tracks';

import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

export const httpAcquisitionPort: AcquisitionPort = {
  retryAcquisition: (...args) => retryAcquisition(...args),
};
