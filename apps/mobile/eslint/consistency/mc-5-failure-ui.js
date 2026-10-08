module.exports = {
  id: 'MC-5',
  description:
    'No Alert.alert or direct showAlert import outside the failure module and the dialog seam',
  baseline: 0,
  config: [
    {
      ignores: [
        'src/shared/ui/failure/**',
        'src/shared/ui/dialog/**',
        'src/shared/ui/confirmDestructive.ts',
        '**/__tests__/**',
      ],
      rules: {
        'no-restricted-properties': [
          'error',
          {
            object: 'Alert',
            property: 'alert',
            message: 'Use showFailureAlert from @shared/ui/failure/showFailure (MC-5).',
          },
        ],
        'no-restricted-imports': [
          'error',
          {
            patterns: [
              {
                group: ['**/dialog/dialog'],
                importNames: ['showAlert'],
                message: 'Use showFailureAlert so the failure is recorded (MC-5).',
              },
            ],
          },
        ],
      },
    },
  ],
  examples: {
    invalid: [
      "Alert.alert('Oops', 'Broke');\n",
      "import { showAlert } from '@shared/ui/dialog/dialog';\n",
    ],
    valid: [
      "showFailureAlert({ surface: 'a.b', title: 'Oops', message: 'Broke' });\n",
      "import { confirm } from './dialog/dialog';\n",
    ],
  },
};
