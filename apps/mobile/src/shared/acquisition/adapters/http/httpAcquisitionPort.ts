import { retryAcquisition } from '@shared/api-client/tracks';

import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

export const httpAcquisitionPort: AcquisitionPort = {
  retryAcquisition: (...args) => retryAcquisition(...args),
};
