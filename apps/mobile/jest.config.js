const preset = require('jest-expo/jest-preset');

const COVERAGE_RATCHET_RAISE_ONLY = {
  statements: 51,
  branches: 45,
  functions: 44,
  lines: 52,
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
  coverageThreshold: { global: COVERAGE_RATCHET_RAISE_ONLY },
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
