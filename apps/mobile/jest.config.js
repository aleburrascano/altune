const preset = require('jest-expo/jest-preset');

const RATCHET_RAISE_ONLY = {
  'src/shared/events/**': { statements: 97, branches: 90, functions: 100, lines: 100 },
  'src/shared/playback/**': { statements: 99, branches: 99, functions: 100, lines: 99 },
  'src/shared/acquisition/**': { statements: 97, branches: 88, functions: 100, lines: 100 },
  'src/shared/offline/**': { statements: 99, branches: 86, functions: 100, lines: 100 },
  'src/shared/api-client/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/shared/telemetry/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/shared/lib/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/shared/auth/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/features/auth/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/features/detail/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/features/discover/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/features/library/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/features/playback/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  'src/features/settings/**': { statements: 0, branches: 0, functions: 0, lines: 0 },
  global: { statements: 0, branches: 0, functions: 0, lines: 0 },
};

module.exports = {
  ...preset,
  rootDir: __dirname,
  testMatch: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
  collectCoverageFrom: [
    'src/**/*.{ts,tsx}',
    '!src/**/__tests__/**',
    '!src/features/_template/**',
    '!src/**/*.d.ts',
  ],
  coverageThreshold: RATCHET_RAISE_ONLY,
  setupFiles: [...(preset.setupFiles ?? []), '<rootDir>/jest/setup-env.js'],
  setupFilesAfterEnv: [...(preset.setupFilesAfterEnv ?? []), '<rootDir>/jest/setup-after-env.js'],
  moduleNameMapper: {
    ...(preset.moduleNameMapper ?? {}),
    '^@/(.*)$': '<rootDir>/src/$1',
    '^@features/(.*)$': '<rootDir>/src/features/$1',
    '^@shared/(.*)$': '<rootDir>/src/shared/$1',
    '^expo/virtual/env$': '<rootDir>/jest/expo-virtual-env.js',
  },
};
