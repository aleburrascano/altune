/**
 * Jest config for the mobile workspace.
 *
 * Bypasses jest-expo's preset because the workspace hoist places jest-expo at
 * the root while react-native ends up under apps/mobile/node_modules; Node's
 * resolution then can't satisfy jest-expo's internal `require('react-native/
 * jest-preset')`. For the API client + hook tests (slices 8-9) we don't need
 * RN's transforms at all — they're pure TypeScript. Slice 10's component
 * test will need RN; that's where we revisit this and either fix the hoist
 * or switch to a per-project nested install.
 *
 * AIDEV-WARNING: don't add tests that import from 'react-native' under this
 * config without first re-enabling the jest-expo preset (and resolving the
 * hoist issue).
 */

module.exports = {
  testEnvironment: 'node',
  rootDir: __dirname,
  testMatch: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
  transform: {
    '^.+\\.tsx?$': [
      'babel-jest',
      {
        // Bypass the app's babel.config.js (which loads babel-preset-expo +
        // module-resolver — not installed at runtime in this jest path).
        configFile: false,
        babelrc: false,
        presets: [require.resolve('@babel/preset-typescript')],
        plugins: [require.resolve('@babel/plugin-transform-modules-commonjs')],
      },
    ],
  },
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx'],
};
