import { Platform } from 'react-native';

export const offlineDownloadsSupported: boolean = Platform.OS !== 'web';
