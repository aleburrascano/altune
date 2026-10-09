'use strict';

const ADAPTERS_SOURCE = '/(^|\\/)adapters(\\/|$)/';

const SELECTOR = `:matches(ImportDeclaration, ExportAllDeclaration, ExportNamedDeclaration)[source.value=${ADAPTERS_SOURCE}]`;

module.exports = {
  id: 'MC-9',
  description:
    'Inside a feature, ui/, hooks/ and ports/ never import adapters/; the provider that supplies the default adapter lives in adapters/',
  config: [
    {
      ignores: [
        'src/shared/**',
        'src/app/**',
        'src/chrome/**',
        'src/features/*/adapters/**',
        'src/features/*/index.ts',
        '**/__tests__/**',
      ],
      rules: {
        'no-restricted-syntax': [
          'error',
          {
            selector: SELECTOR,
            message:
              'Imports point inward: ui/, hooks/ and ports/ do not import adapters/. Reach the adapter through a port; the provider that supplies the default adapter lives in adapters/ (MC-9, module-shape M3).',
          },
        ],
      },
    },
  ],
  examples: {
    invalid: [
      "import { reportSignInFailure } from '../adapters/http/reportSignInFailure';\nexport const r = reportSignInFailure;\n",
      "import { httpLibraryPort } from '@features/library/adapters/http/httpLibraryPort';\nexport const p = httpLibraryPort;\n",
      "export { httpLibraryPort } from '../adapters/http/httpLibraryPort';\n",
    ],
    valid: [
      "import { useLibraryPort } from '../ports/LibraryPortProvider';\nexport const u = useLibraryPort;\n",
      "import type { LibraryPort } from '../ports/libraryPort';\nexport type T = LibraryPort;\n",
    ],
  },
};
