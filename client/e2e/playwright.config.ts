import { defineConfig } from '@playwright/test';

const saveJourney = 'save-acquire-library.spec.ts';
const playJourney = 'play-from-library.spec.ts';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  outputDir: 'test-results',
  timeout: 90_000,
  use: {
    baseURL: process.env.WEB_URL,
    navigationTimeout: 60_000,
    trace: 'retain-on-failure',
    ignoreHTTPSErrors: true,
  },
  projects: [
    { name: 'journeys', testIgnore: [saveJourney, playJourney] },
    { name: 'save', testMatch: saveJourney },
    { name: 'play', testMatch: playJourney, dependencies: ['save'] },
  ],
});
