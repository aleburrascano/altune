/**
 * Jest config for the mobile app.
 *
 * Uses jest-expo's preset for RN-aware test transforms. The preset's internal
 * `require('react-native/jest-preset')` resolves cleanly because apps/mobile is
 * a standalone npm package (ADR-0016) — jest-expo and react-native share one
 * flat node_modules with no workspace hoisting to split them. (This previously
 * needed an `.npmrc install-strategy=nested` hack; decoupling removed the need.)
 *
 * Path aliases (`@/`, `@features/`, `@shared/`) are mapped here for jest;
 * babel.config.js handles them for Metro at build time.
 */

const preset = require('jest-expo/jest-preset');

module.exports = {
  ...preset,
  rootDir: __dirname,
  testMatch: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
  setupFiles: [...(preset.setupFiles ?? []), '<rootDir>/jest/setup-env.js'],
  setupFilesAfterEnv: [
    ...(preset.setupFilesAfterEnv ?? []),
    '<rootDir>/jest/setup-after-env.js',
  ],
  moduleNameMapper: {
    ...(preset.moduleNameMapper ?? {}),
    '^@/(.*)$': '<rootDir>/src/$1',
    '^@features/(.*)$': '<rootDir>/src/features/$1',
    '^@shared/(.*)$': '<rootDir>/src/shared/$1',
    // babel-preset-expo rewrites `process.env.EXPO_PUBLIC_*` reads as
    // `require('expo/virtual/env').EXPO_PUBLIC_*` for the Metro bundle; that
    // virtual module doesn't exist outside Metro. Mock it in jest with the
    // file below so the real process.env is exposed to test code.
    '^expo/virtual/env$': '<rootDir>/jest/expo-virtual-env.js',
  },
};
