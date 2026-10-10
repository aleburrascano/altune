import { cleanupAsync } from '@testing-library/react-native';

const ReactNative = require('react-native');
const { __http } = require('./doubles/fetch.js');

const WARM_UP_TIMEOUT_MS = 30000;

const LAZY_REACT_NATIVE_EXPORTS_A_FIRST_RENDER_READS = [
  'AccessibilityInfo',
  'ActivityIndicator',
  'Alert',
  'Animated',
  'AppState',
  'FlatList',
  'Modal',
  'Pressable',
  'ScrollView',
  'Text',
  'TextInput',
  'useWindowDimensions',
] as const;

function requireLazyExportsWhileTheTestFileImportsNotInsideTheTimedHook(): void {
  for (const name of LAZY_REACT_NATIVE_EXPORTS_A_FIRST_RENDER_READS) void ReactNative[name];
}

export function warmUpFirstRender(warmUp: () => Promise<void>): void {
  requireLazyExportsWhileTheTestFileImportsNotInsideTheTimedHook();

  beforeAll(async () => {
    try {
      await warmUp();
    } finally {
      await cleanupAsync();
      __http.reset();
    }
  }, WARM_UP_TIMEOUT_MS);
}
