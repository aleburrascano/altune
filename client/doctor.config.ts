const pagingLoopsAwaitInOrder = {
  files: [
    'src/features/detail/hooks/useLibraryMatches.ts',
    'src/features/library/hooks/useLibraryTracks.ts',
    'src/shared/apiClient/tracks.ts',
  ],
  rules: ['react-doctor/async-await-in-loop'],
};

const sequentialByDesignAwaitInLoop = {
  files: [
    'src/shared/offline/state/pinnedDownloadWorker.ts',
    'src/shared/offline/state/pinnedStore.ts',
    'src/shared/telemetry/outbox.ts',
  ],
  rules: ['react-doctor/async-await-in-loop'],
};

const downloadAwaitedBeforeCancelCheck = {
  files: ['src/shared/offline/state/pinnedDownloadWorker.ts'],
  rules: ['react-doctor/async-defer-await'],
};

const queueResumeCoalescesSequentially = {
  files: ['src/features/playback/hooks/useQueueResume.ts'],
  rules: ['react-doctor/async-await-in-loop'],
};

const nativeTailDrainRebuildsInOrder = {
  files: ['src/features/playback/native/rebuildNativeTail.ts'],
  rules: ['react-doctor/async-await-in-loop'],
};

const resetNativeRunsEvenWhenEmpty = {
  files: ['src/features/playback/native/loadNativeTrack.ts'],
  rules: ['react-doctor/async-defer-await'],
};

const imageOnlyResolvesAssetSource = {
  files: ['src/features/playback/native/nativeTrack.ts'],
  rules: ['react-doctor/rn-prefer-expo-image'],
};

const failureShownIsAnImpressionLogSyncedInEffect = {
  files: [
    'src/features/detail/ui/TrackStatusBanners.tsx',
    'src/features/library/ui/LibraryRowFailure.tsx',
    'src/shared/ui/primitives/Banner.tsx',
    'src/features/discovery/hooks/useReportQueryFailure.ts',
  ],
  rules: ['react-doctor/no-event-handler'],
};

const bannerChildrenOnlyPicksTelemetryMessage = {
  files: ['src/shared/ui/primitives/Banner.tsx'],
  rules: ['react-doctor/no-polymorphic-children'],
};

const recordEventIsFireAndForgetAndReadsNoQueryCache = {
  files: ['src/shared/telemetry/useRecordEvent.ts'],
  rules: ['react-doctor/query-mutation-missing-invalidation'],
};

const mediaSessionEffectsSyncTheBrowserMediaSession = {
  files: ['src/features/playback/hooks/useMediaSession.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const rememberTrackSyncsNativeTrackPlayer = {
  files: ['src/features/playback/native/TrackPlayerPlaybackProvider.tsx'],
  rules: ['react-doctor/no-event-handler'],
};

const progressValueFollowsPlaybackPosition = {
  files: ['src/features/playback/hooks/useScrubberGesture.ts'],
  rules: ['react-doctor/no-pass-data-to-parent'],
};

const closeGuardsClearOnlyOnceTheParentHasClosedTheSurface = {
  files: ['src/shared/playlists/hooks/playlist/useSingleFlightAction.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const remainingPlaylistsLoadFollowsLoadedPages = {
  files: ['src/features/library/hooks/usePlaylistsView.tsx'],
  rules: ['react-doctor/no-event-handler'],
};

const slateExpiryArrivesAsServerData = {
  files: ['src/features/discovery/hooks/useSearchRefresh.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const screenReaderAnnouncementSyncsAnExternalSystem = {
  files: ['src/shared/ui/useAnnounceChange.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const relatedRailIsCappedAtTwentyTracks = {
  files: ['src/features/detail/ui/RelatedTracksSection.tsx'],
  rules: ['react-doctor/rn-no-scrollview-mapped-list'],
};

const libraryChipsRailIsTheFixedChipsConstant = {
  files: ['src/features/library/ui/LibraryChips.tsx'],
  rules: ['react-doctor/rn-no-scrollview-mapped-list'],
};

const passwordHintIsFormTextNotACredential = {
  files: ['src/features/auth/domain/validation.ts'],
  rules: ['react-doctor/no-secrets-in-client-code'],
};

const resultSignatureIsASearchResultIdentityNotACryptoSignature = {
  files: ['src/features/discovery/hooks/useResultTap.ts'],
  rules: ['react-doctor/insecure-crypto-risk'],
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
          'src/shell/platformExtras.tsx',
          'src/features/auth/hooks/useAuthDeepLink.ts',
          'src/features/auth/hooks/useOAuth.ts',
          'src/features/auth/ui/AuthCallbackScreen.tsx',
          'src/features/playback/adapters/PlaybackProvider.tsx',
          'src/features/playback/domain/playsThroughTrackPlayer.ts',
          'src/shared/auth/authStorage.ts',
          'src/shared/device/isWebPlatform.ts',
          'src/shared/device/webOrigin.ts',
          'src/shared/files/defaultFileStore.ts',
          'src/shared/ui/announce.ts',
          'src/shared/ui/dialog/dialog.ts',
          'src/shared/ui/haptics.ts',
          'src/shared/ui/keyboard/keyboard.ios.tsx',
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
        files: [
          'src/shared/ui/keyboard/keyboard.ios.tsx',
          'src/shared/ui/keyboard/keyboard.tsx',
          'src/shared/ui/keyboard/keyboard.web.tsx',
        ],
        rules: ['react-doctor/only-export-components'],
      },
      pagingLoopsAwaitInOrder,
      sequentialByDesignAwaitInLoop,
      downloadAwaitedBeforeCancelCheck,
      queueResumeCoalescesSequentially,
      nativeTailDrainRebuildsInOrder,
      resetNativeRunsEvenWhenEmpty,
      imageOnlyResolvesAssetSource,
      failureShownIsAnImpressionLogSyncedInEffect,
      bannerChildrenOnlyPicksTelemetryMessage,
      mediaSessionEffectsSyncTheBrowserMediaSession,
      rememberTrackSyncsNativeTrackPlayer,
      recordEventIsFireAndForgetAndReadsNoQueryCache,
      progressValueFollowsPlaybackPosition,
      closeGuardsClearOnlyOnceTheParentHasClosedTheSurface,
      remainingPlaylistsLoadFollowsLoadedPages,
      slateExpiryArrivesAsServerData,
      screenReaderAnnouncementSyncsAnExternalSystem,
      relatedRailIsCappedAtTwentyTracks,
      libraryChipsRailIsTheFixedChipsConstant,
      passwordHintIsFormTextNotACredential,
      resultSignatureIsASearchResultIdentityNotACryptoSignature,
    ],
  },
};
