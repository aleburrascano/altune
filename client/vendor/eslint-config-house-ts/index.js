const tsParser = require('@typescript-eslint/parser');
const tsPlugin = require('@typescript-eslint/eslint-plugin');

const TYPESCRIPT_FILES = ['**/*.{ts,tsx,mts,cts}'];

const NUMBERS_THAT_NEED_NO_NAME = [-1, 0, 1];

const FUNCTION_NODES = ':matches(FunctionDeclaration, FunctionExpression, ArrowFunctionExpression, TSDeclareFunction, TSMethodSignature, TSFunctionType)';

const NAME_THE_SHAPE =
  'Name this shape: an inline object type in a signature is a riddle. Declare a named type or interface (TrackSummary, TrackRowProps) and use it here.';

const NO_SILENT_FALLBACK =
  'No silent fallback: an empty [] or {} makes "failed to load" look like "loaded, and empty". Decide the default once in the boundary schema, or model the state as a discriminated union.';

const inlineObjectTypesInSignatures = [
  `${FUNCTION_NODES} > TSTypeAnnotation.returnType TSTypeLiteral`,
  `${FUNCTION_NODES} > :matches(Identifier, ObjectPattern, ArrayPattern, AssignmentPattern, RestElement).params TSTypeLiteral`,
];

const relativeImportSources = [
  ':matches(ImportDeclaration, ExportNamedDeclaration, ExportAllDeclaration, ImportExpression) > Literal.source[value=/^[.][.]?([/]|$)/]',
];

const emptyCollectionFallbacks = [
  'LogicalExpression[operator=/^(\\?\\?|\\|\\|)$/] > ArrayExpression.right[elements.length=0]',
  'LogicalExpression[operator=/^(\\?\\?|\\|\\|)$/] > ObjectExpression.right[properties.length=0]',
  'ConditionalExpression > ArrayExpression[elements.length=0]',
  'ConditionalExpression > ObjectExpression[properties.length=0]',
];

function ruleReportingEvery(selectors, message) {
  return {
    meta: { type: 'suggestion', schema: [] },
    create(context) {
      function reportAt(node) {
        context.report({ node, message });
      }
      return Object.fromEntries(selectors.map((selector) => [selector, reportAt]));
    },
  };
}

const house = {
  meta: { name: 'house' },
  rules: {
    'no-inline-object-types': ruleReportingEvery(inlineObjectTypesInSignatures, NAME_THE_SHAPE),
    'no-empty-fallbacks': ruleReportingEvery(emptyCollectionFallbacks, NO_SILENT_FALLBACK),
    'no-unnamed-regex': ruleReportingEvery(
      ['Literal[regex]:not(VariableDeclarator > Literal.init)'],
      'Name this regex: declare it as a constant whose name says what it matches (SPOTIFY_TRACK_URL_PATTERN), or wrap it in a named predicate (isSpotifyTrackUrl).',
    ),
    'no-double-cast': ruleReportingEvery(
      ['TSAsExpression > TSAsExpression.expression > TSUnknownKeyword.typeAnnotation'],
      '"as unknown as" tells the compiler to stop checking. Parse the value with its zod schema at the boundary instead.',
    ),
    'no-relative-imports': ruleReportingEvery(
      relativeImportSources,
      'Import through the repo alias, never a relative path: @/features/library/trackRow in a bundled app, #src/core/board.ts in a repo Node runs directly (Node only allows # aliases, set in package.json "imports").',
    ),
    'tests-in-tests-folder': ruleReportingEvery(
      ['Program'],
      'Move this test into a __tests__ folder beside the unit it tests (pinnedStore.ts -> __tests__/pinnedStore.test.ts).',
    ),
  },
};

const houseStyle = {
  name: 'houseStyle',
  files: TYPESCRIPT_FILES,
  plugins: { '@typescript-eslint': tsPlugin, house },
  languageOptions: { parser: tsParser, parserOptions: { ecmaFeatures: { jsx: true } } },
  rules: {
    'func-style': ['error', 'declaration'],
    'no-nested-ternary': 'error',
    'house/no-relative-imports': 'error',
    'no-magic-numbers': 'off',
    '@typescript-eslint/no-magic-numbers': [
      'error',
      {
        ignore: NUMBERS_THAT_NEED_NO_NAME,
        ignoreArrayIndexes: true,
        ignoreEnums: true,
        ignoreNumericLiteralTypes: true,
        ignoreReadonlyClassProperties: true,
        ignoreTypeIndexes: true,
      },
    ],
    'house/no-inline-object-types': 'error',
    'house/no-empty-fallbacks': 'error',
    'house/no-unnamed-regex': 'error',
    'house/no-double-cast': 'error',
  },
};

const testFilesLiveInTestsFolders = {
  name: 'testFilesLiveInTestsFolders',
  files: ['**/*.test.{ts,tsx}'],
  ignores: ['**/__tests__/**'],
  rules: { 'house/tests-in-tests-folder': 'error' },
};

const typedChecks = {
  name: 'typedChecks',
  files: TYPESCRIPT_FILES,
  languageOptions: { parserOptions: { projectService: true } },
  rules: { '@typescript-eslint/switch-exhaustiveness-check': 'error' },
};

module.exports = {
  plugin: house,
  configs: {
    recommended: [houseStyle, testFilesLiveInTestsFolders],
    typed: [typedChecks],
  },
};
