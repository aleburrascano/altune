const { assertSupportedNode } = require('./jest/requireNode22');

assertSupportedNode(process.versions.node);

const preset = require('jest-expo/jest-preset');
const webPreset = require('jest-expo/web/jest-preset');

const PREBUILT_COMMONJS_RENDERER_BABEL_WOULD_NOT_CHANGE =
  '/node_modules/react-native/Libraries/Renderer/implementations/';

const WEB_INTEGRATION_TESTS = '\\.web\\.integration\\.test\\.tsx?$';

const tsconfigAliases = Object.fromEntries(
  Object.entries(require('./tsconfig.json').compilerOptions.paths).map(([alias, [target]]) => [
    `^${alias.replace('/*', '/(.*)')}$`,
    `<rootDir>/${target.replace('./', '').replace('/*', '/$1')}`,
  ]),
);

const houseModuleNameMapper = {
  ...tsconfigAliases,
  '^expo/virtual/env$': '<rootDir>/jest/expo-virtual-env.js',
  '^react-native-safe-area-context$': '<rootDir>/jest/doubles/react-native-safe-area-context.js',
  '^react-native-reanimated$': '<rootDir>/jest/doubles/react-native-reanimated.js',
  '^react-native-gesture-handler/ReanimatedSwipeable$':
    '<rootDir>/jest/doubles/reanimated-swipeable.js',
};

const ESM_ONLY_DEPENDENCIES = 'decode-uri-component';

function allowEsmOnlyDependencies(pattern) {
  return pattern.replace('standard-navigation))', `standard-navigation|${ESM_ONLY_DEPENDENCIES}))`);
}

const nativeProject = {
  ...preset,
  displayName: 'native',
  rootDir: __dirname,
  restoreMocks: true,
  transformIgnorePatterns: [
    ...preset.transformIgnorePatterns.map(allowEsmOnlyDependencies),
    PREBUILT_COMMONJS_RENDERER_BABEL_WOULD_NOT_CHANGE,
  ],
  testMatch: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
  testPathIgnorePatterns: [...(preset.testPathIgnorePatterns ?? []), WEB_INTEGRATION_TESTS],
  setupFiles: [...(preset.setupFiles ?? []), '<rootDir>/jest/setup-env.js'],
  setupFilesAfterEnv: [...(preset.setupFilesAfterEnv ?? []), '<rootDir>/jest/setup-after-env.js'],
  moduleNameMapper: { ...(preset.moduleNameMapper ?? {}), ...houseModuleNameMapper },
};

const webProject = {
  ...webPreset,
  displayName: 'web',
  rootDir: __dirname,
  restoreMocks: true,
  transformIgnorePatterns: webPreset.transformIgnorePatterns.map(allowEsmOnlyDependencies),
  testMatch: [
    '**/__tests__/**/*.web.integration.test.ts',
    '**/__tests__/**/*.web.integration.test.tsx',
  ],
  setupFiles: [...(webPreset.setupFiles ?? []), '<rootDir>/jest/setup-env.js'],
  setupFilesAfterEnv: [
    ...(webPreset.setupFilesAfterEnv ?? []),
    '<rootDir>/jest/setup-after-env.js',
  ],
  moduleNameMapper: {
    ...(webPreset.moduleNameMapper ?? {}),
    ...houseModuleNameMapper,
    '^lucide-react-native$':
      '<rootDir>/node_modules/lucide-react-native/dist/cjs/lucide-react-native.js',
  },
};

const RESTART_WORKER_ABOVE_SO_EARLYOOM_DOES_NOT_KILL_IT = '1G';
const WORKERS_THAT_SHARE_THE_CI_BOX_WITH_TWO_RUNNERS = 2;

module.exports = {
  rootDir: __dirname,
  randomize: true,
  forceExit: true,
  maxWorkers: WORKERS_THAT_SHARE_THE_CI_BOX_WITH_TWO_RUNNERS,
  workerIdleMemoryLimit: RESTART_WORKER_ABOVE_SO_EARLYOOM_DOES_NOT_KILL_IT,
  projects: [nativeProject, webProject],
};
