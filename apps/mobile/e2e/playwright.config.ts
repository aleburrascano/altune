import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  outputDir: 'test-results',
  use: {
    baseURL: process.env.WEB_URL,
    trace: 'retain-on-failure',
  },
});
