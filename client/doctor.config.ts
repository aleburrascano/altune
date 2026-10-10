const pagingLoopsAwaitInOrder = {
  files: [
    'src/features/detail/hooks/useLibraryMatches.ts',
    'src/features/library/hooks/useLibraryTracks.ts',
    'src/shared/apiClient/tracks.ts',
  ],
  rules: ['react-doctor/async-await-in-loop'],
};

export default {
  rules: {
    'react-doctor/rn-no-raw-text': 'off',
    'react-doctor/rn-prefer-reanimated': 'off',
    'react-doctor/no-multi-comp': 'off',
    'react-doctor/jsx-no-jsx-as-prop': 'off',
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
          'src/shared/ui/announce.ts',
          'src/shared/ui/dialog/dialog.ts',
          'src/shared/ui/haptics.ts',
          'src/shared/ui/keyboard/keyboard.tsx',
        ],
        rules: ['deslop/unused-export'],
      },
      {
        files: ['eslint/**', 'jest/**', 'src/**/__tests__/**'],
        rules: [
          'react-doctor/js-combine-iterations',
          'react-doctor/no-dynamic-import-path',
          'react-doctor/js-set-map-lookups',
          'react-doctor/no-json-parse-stringify-clone',
        ],
      },
      {
        files: ['eslint/consistency/**'],
        rules: ['deslop/unused-file'],
      },
      {
        files: ['src/shared/ui/keyboard/keyboard.tsx', 'src/shared/ui/keyboard/keyboard.web.tsx'],
        rules: ['react-doctor/only-export-components'],
      },
      pagingLoopsAwaitInOrder,
    ],
  },
};
