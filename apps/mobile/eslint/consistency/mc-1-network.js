module.exports = {
  id: 'MC-1',
  description: 'All network calls go through apiFetch or a named transport seam',
  baseline: 9,
  config: [
    {
      ignores: [
        'src/shared/api-client/index.ts',
        'src/shared/auth/authDeadline.ts',
        'src/shared/killSwitch/killSwitchPoll.ts',
        'src/shared/events/sse-client.ts',
        'src/shared/files/fileStore.ts',
        'src/features/auth/testAuth.ts',
        'src/features/auth/reportSignInFailure.ts',
        '**/__tests__/**',
      ],
      rules: {
        'no-restricted-globals': [
          'error',
          ...['fetch', 'XMLHttpRequest', 'EventSource', 'WebSocket'].map((name) => ({
            name,
            message: `Use apiFetch/apiSend from @shared/api-client, not raw ${name} (MC-1).`,
          })),
        ],
        'no-restricted-syntax': [
          'error',
          {
            selector: "MemberExpression[object.name='File'][property.name='downloadFileAsync']",
            message: 'Download through the FileStore port (@shared/files/fileStore) (MC-1).',
          },
        ],
      },
    },
    {
      ignores: [
        'src/shared/auth/**',
        'src/shared/api-client/**',
        'src/shared/events/**',
        '**/__tests__/**',
      ],
      rules: {
        'no-restricted-imports': [
          'error',
          {
            paths: [
              {
                name: 'axios',
                message: 'Use apiFetch/apiSend from @shared/api-client (MC-1).',
              },
              {
                name: '@shared/auth/supabaseClient',
                message: 'Reach auth through shared/auth, not supabaseClient directly (MC-1).',
              },
            ],
          },
        ],
      },
    },
  ],
  examples: {
    invalid: [
      "export const r = () => fetch('/x');\n",
      'export const x = new XMLHttpRequest();\n',
      "import axios from 'axios';\nexport const a = axios;\n",
      "import { supabase } from '@shared/auth/supabaseClient';\nexport const s = supabase;\n",
      "import { File } from 'expo-file-system';\nexport const d = () => File.downloadFileAsync('u', new File('f'));\n",
    ],
    valid: [
      "import { apiFetch } from '@shared/api-client';\nexport const r = () => apiFetch('/x');\n",
    ],
  },
};
