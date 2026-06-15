import type { AcquisitionStatus } from '@shared/api-client/types';

export function canPlay(acquisitionStatus: AcquisitionStatus | undefined | null): boolean {
  return acquisitionStatus === 'ready';
}
