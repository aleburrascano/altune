import { cleanupAsync } from '@testing-library/react-native';

const { __http } = require('./doubles/fetch.js');

export const WARM_UP_TIMEOUT_MS = 30000;

export function warmUpFirstRender(warmUp: () => Promise<void>): void {
  beforeAll(async () => {
    try {
      await warmUp();
    } finally {
      await cleanupAsync();
      __http.reset();
    }
  }, WARM_UP_TIMEOUT_MS);
}
