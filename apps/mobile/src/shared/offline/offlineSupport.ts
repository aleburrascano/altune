import { isWebPlatform } from '@shared/device/device';

export const offlineDownloadsSupported: boolean = !isWebPlatform();
