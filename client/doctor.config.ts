const pagingLoopsAwaitInOrder = {
  files: [
    'src/features/detail/core/useLibraryMatches.ts',
    'src/features/library/view/useLibraryTracks.ts',
    'src/shared/apiClient/adapters/tracks.ts',
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
  files: ['src/features/player/adapters/native/useQueueResume.ts'],
  rules: ['react-doctor/async-await-in-loop'],
};

const nativeTailDrainRebuildsInOrder = {
  files: ['src/features/player/adapters/native/rebuildNativeTail.ts'],
  rules: ['react-doctor/async-await-in-loop'],
};

const resetNativeRunsEvenWhenEmpty = {
  files: ['src/features/player/adapters/native/loadNativeTrack.ts'],
  rules: ['react-doctor/async-defer-await'],
};

const moduleDoorsWaitForCallersToMoveOffInternals = {
  files: ['src/shared/*/index.ts'],
  rules: ['deslop/unused-file'],
};

const imageOnlyResolvesAssetSource = {
  files: ['src/features/player/adapters/native/nativeTrack.ts'],
  rules: ['react-doctor/rn-prefer-expo-image'],
};

const failureShownIsAnImpressionLogSyncedInEffect = {
  files: [
    'src/features/detail/view/TrackStatusBanners.tsx',
    'src/features/library/view/LibraryRowFailure.tsx',
    'src/shared/ui/view/primitives/Banner.tsx',
    'src/features/discovery/view/search/useReportQueryFailure.ts',
  ],
  rules: ['react-doctor/no-event-handler'],
};

const bannerChildrenOnlyPicksTelemetryMessage = {
  files: ['src/shared/ui/view/primitives/Banner.tsx'],
  rules: ['react-doctor/no-polymorphic-children'],
};

const recordEventIsFireAndForgetAndReadsNoQueryCache = {
  files: ['src/shared/telemetry/shell/useRecordEvent.ts'],
  rules: ['react-doctor/query-mutation-missing-invalidation'],
};

const mediaSessionEffectsSyncTheBrowserMediaSession = {
  files: ['src/features/player/view/useMediaSession.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const rememberTrackSyncsNativeTrackPlayer = {
  files: ['src/features/player/adapters/native/TrackPlayerPlaybackProvider.tsx'],
  rules: ['react-doctor/no-event-handler'],
};

const progressValueFollowsPlaybackPosition = {
  files: ['src/features/player/view/useScrubberGesture.ts'],
  rules: ['react-doctor/no-pass-data-to-parent'],
};

const closeGuardsClearOnlyOnceTheParentHasClosedTheSurface = {
  files: ['src/shared/playlists/view/playlist/useSingleFlightAction.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const remainingPlaylistsLoadFollowsLoadedPages = {
  files: ['src/features/library/view/usePlaylistsView.tsx'],
  rules: ['react-doctor/no-event-handler'],
};

const slateExpiryArrivesAsServerData = {
  files: ['src/features/discovery/view/search/useSearchRefresh.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const screenReaderAnnouncementSyncsAnExternalSystem = {
  files: ['src/shared/ui/view/useAnnounceChange.ts'],
  rules: ['react-doctor/no-event-handler'],
};

const relatedRailIsCappedAtTwentyTracks = {
  files: ['src/features/detail/view/RelatedTracksSection.tsx'],
  rules: ['react-doctor/rn-no-scrollview-mapped-list'],
};

const libraryChipsRailIsTheFixedChipsConstant = {
  files: ['src/features/library/view/LibraryChips.tsx'],
  rules: ['react-doctor/rn-no-scrollview-mapped-list'],
};

const passwordHintIsFormTextNotACredential = {
  files: ['src/features/signIn/core/validation.ts'],
  rules: ['react-doctor/no-secrets-in-client-code'],
};

const resultSignatureIsASearchResultIdentityNotACryptoSignature = {
  files: ['src/features/discovery/hooks/results/useResultTap.ts'],
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
          'src/mobile/frame/platformExtras.tsx',
          'src/mobile/deviceFileStore.ts',
          'src/features/signIn/shell/useAuthDeepLink.ts',
          'src/features/signIn/hooks/useOAuth.ts',
          'src/features/signIn/ui/AuthCallbackScreen.tsx',
          'src/features/player/adapters/PlaybackProvider.tsx',
          'src/features/player/domain/playsThroughTrackPlayer.ts',
          'src/mobile/authStorage.ts',
          'src/shared/device/core/isWebPlatform.ts',
          'src/shared/device/core/webOrigin.ts',
          'src/shared/ui/view/a11y/announce.ts',
          'src/shared/ui/view/dialog/dialog.ts',
          'src/shared/ui/haptics.ts',
          'src/shared/ui/view/keyboard/keyboard.ios.tsx',
          'src/shared/ui/view/keyboard/keyboard.tsx',
          'src/shared/ui/view/primitives/HorizontalRail.tsx',
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
        files: [
          'src/shared/ui/view/keyboard/keyboard.ios.tsx',
          'src/shared/ui/view/keyboard/keyboard.tsx',
          'src/shared/ui/view/keyboard/keyboard.web.tsx',
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
      moduleDoorsWaitForCallersToMoveOffInternals,
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
