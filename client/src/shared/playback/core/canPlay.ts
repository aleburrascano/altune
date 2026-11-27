import { isReady } from '@shared/catalog';
export function canPlay(acquisitionStatus: string | undefined | null): boolean {
  return isReady(acquisitionStatus);
}
