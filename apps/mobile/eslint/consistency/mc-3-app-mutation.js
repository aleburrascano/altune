module.exports = {
  id: 'MC-3',
  description: 'No raw useMutation imports from @tanstack/react-query outside the telemetry seams',
  baseline: 0,
  config: [
    {
      ignores: [
        'src/shared/query/useAppMutation.ts',
        'src/shared/telemetry/useRecordEvent.ts',
        '**/__tests__/**',
      ],
      rules: {
        'no-restricted-imports': [
          'error',
          {
            paths: [
              {
                name: '@tanstack/react-query',
                importNames: ['useMutation'],
                message:
                  'Use useAppMutation from @shared/query/useAppMutation so the user action is recorded (MC-3).',
              },
            ],
          },
        ],
      },
    },
  ],
  examples: {
    invalid: ["import { useMutation } from '@tanstack/react-query';\n"],
    valid: ["import { useQueryClient } from '@tanstack/react-query';\n"],
  },
};
