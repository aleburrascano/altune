'use strict';

module.exports = {
  id: 'MC-10',
  description: 'Query keys come from the one query-key factory, never a literal array in a feature',
  config: [
    {
      ignores: ['src/shared/**', 'src/app/**', 'src/app-shell/**', '**/__tests__/**'],
      rules: {
        'no-restricted-syntax': [
          'error',
          {
            selector:
              'Property[key.name="queryKey"]:matches([value.type="ArrayExpression"], [value.expression.type="ArrayExpression"])',
            message:
              'Query keys come from the query-key factory, not a literal queryKey array (MC-10, module-shape M5).',
          },
          {
            selector: 'CallExpression[callee.name="queryOptions"]',
            message:
              'queryOptions() builds its own key; define it in the query-key factory (MC-10, module-shape M5).',
          },
        ],
      },
    },
  ],
  examples: {
    invalid: [
      "export const o = { queryKey: ['x'] };\n",
      "export const o = { queryKey: ['x', 1] as const };\n",
      "import { queryOptions } from '@tanstack/react-query';\nexport const o = queryOptions({ queryKey: keyFor(), queryFn });\n",
    ],
    valid: [
      "import { relatedTracksKey } from '@shared/lib/query-keys';\nexport const o = { queryKey: relatedTracksKey('a') };\n",
    ],
  },
};
