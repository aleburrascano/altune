'use strict';

const NETWORK_FREE_MODULES = ['ids', 'trackAcquisition', 'types', 'wireDecoders'];

const API_CLIENT_SOURCE_DOT_FOR_SLASH = `^(@shared|(\\.{1,2}.)+.*shared).api-client(.(?!(${NETWORK_FREE_MODULES.join('|')})$)[^.]+)?$`;

const VALUE_SPECIFIER =
  ':matches(ImportDefaultSpecifier, ImportNamespaceSpecifier, ImportSpecifier[importKind!="type"])';

const SELECTOR = `ImportDeclaration[importKind!="type"][source.value=/${API_CLIENT_SOURCE_DOT_FOR_SLASH}/]:has(${VALUE_SPECIFIER})`;

module.exports = {
  id: 'MC-8',
  description:
    'Features reach the API only through their own adapters, never a value import from shared/api-client',
  baseline: 17,
  config: [
    {
      ignores: [
        'src/shared/**',
        'src/app/**',
        'src/app-shell/**',
        'src/features/*/adapters/**',
        '**/__tests__/**',
      ],
      rules: {
        'no-restricted-syntax': [
          'error',
          {
            selector: SELECTOR,
            message:
              'Features reach the API through their own adapters, not a value import from shared/api-client (MC-8). Type imports and ids/trackAcquisition/types/wireDecoders are allowed.',
          },
        ],
      },
    },
  ],
  examples: {
    invalid: [
      "import { getTracks } from '@shared/api-client';\nexport const g = getTracks;\n",
      "import { getTracks } from '@shared/api-client/tracks';\nexport const g = getTracks;\n",
      "import { getTracks } from '../../shared/api-client/tracks';\nexport const g = getTracks;\n",
      "import { type TrackResponse, getTracks } from '@shared/api-client';\nexport const g = getTracks;\n",
    ],
    valid: [
      "import type { TrackResponse } from '@shared/api-client/types';\nexport type T = TrackResponse;\n",
      "import type { TrackResponse } from '@shared/api-client';\nexport type T = TrackResponse;\n",
      "import { type TrackResponse } from '@shared/api-client';\nexport type T = TrackResponse;\n",
      "import { trackIdOf } from '@shared/api-client/ids';\nexport const t = trackIdOf;\n",
    ],
  },
};
