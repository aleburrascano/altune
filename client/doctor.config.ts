export default {
  rules: {
    'react-doctor/rn-no-raw-text': 'off',
    'react-doctor/rn-prefer-reanimated': 'off',
    'deslop/unused-dev-dependency': 'off',
  },
  ignore: {
    overrides: [
      {
        files: [
          'src/chrome/platformExtras.tsx',
          'src/features/auth/hooks/useAuthDeepLink.ts',
          'src/features/auth/hooks/useOAuth.ts',
          'src/features/auth/ui/AuthCallbackScreen.tsx',
          'src/features/playback/adapters/PlaybackProvider.tsx',
          'src/features/playback/domain/playsThroughTrackPlayer.ts',
          'src/shared/auth/authStorage.ts',
          'src/shared/device/webOrigin.ts',
          'src/shared/files/defaultFileStore.ts',
          'src/shared/ui/dialog/dialog.ts',
          'src/shared/ui/haptics.ts',
          'src/shared/ui/keyboard/keyboard.tsx',
        ],
        rules: ['deslop/unused-export'],
      },
      {
        files: ['eslint/consistency/**'],
        rules: ['deslop/unused-file'],
      },
      {
        files: ['src/shared/ui/keyboard/keyboard.tsx', 'src/shared/ui/keyboard/keyboard.web.tsx'],
        rules: ['react-doctor/only-export-components'],
      },
    ],
  },
};
