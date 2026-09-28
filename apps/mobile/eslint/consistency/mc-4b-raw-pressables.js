module.exports = {
  id: 'MC-4b',
  description: 'No raw Pressable or Touchable* imports from react-native outside shared/ui primitives',
  baseline: 51,
  config: [
    {
      ignores: ['src/shared/ui/primitives/**', 'src/shared/ui/navigation/**', '**/__tests__/**'],
      rules: {
        'no-restricted-imports': [
          'error',
          {
            paths: [
              {
                name: 'react-native',
                importNames: [
                  'Pressable',
                  'TouchableOpacity',
                  'TouchableHighlight',
                  'TouchableWithoutFeedback',
                  'TouchableNativeFeedback',
                ],
                message:
                  'Use a shared/ui primitive (Button, IconButton, Chip, Row) so taps are recorded (MC-4b).',
              },
            ],
          },
        ],
      },
    },
  ],
  examples: {
    invalid: ["import { Pressable } from 'react-native';\n"],
    valid: ["import { View } from 'react-native';\n"],
  },
};
