import { Platform } from 'react-native';

import { isExpoGo } from '@shared/playback/isExpoGo';

export const playsThroughTrackPlayer = !isExpoGo && Platform.OS !== 'web';
