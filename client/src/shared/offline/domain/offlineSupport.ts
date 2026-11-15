import { isWebPlatform } from '@shared/device';

export const offlineDownloadsSupported: boolean = !isWebPlatform();
