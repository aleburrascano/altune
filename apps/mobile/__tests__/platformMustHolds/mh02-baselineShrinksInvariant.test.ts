import { Linter } from 'eslint';
import * as fs from 'node:fs';
import * as path from 'node:path';

type Baseline = { ui: string[]; logic: string[] };

const CWD = path.join(__dirname, '..', '..');
const FROZEN_BASELINE: Record<string, Baseline> = {
  'src/app/platform-legacy.json': {
    ui: ['src/app/(tabs)/_layout.tsx', 'src/app/_layout.tsx'],
    logic: [],
  },
  'src/features/auth/platform-legacy.json': {
    ui: [
      'src/features/auth/ui/AuthCallbackScreen.tsx',
      'src/features/auth/ui/hero/AuthHeroLayout.tsx',
    ],
    logic: [
      'src/features/auth/completeAuthIntent.ts',
      'src/features/auth/hooks/useAuthDeepLink.ts',
      'src/features/auth/hooks/useOAuth.ts',
      'src/features/auth/parseAuthLink.ts',
    ],
  },
  'src/features/detail/platform-legacy.json': {
    ui: [],
    logic: [
      'src/features/detail/detailHealth.ts',
      'src/features/detail/hooks/useLateralNav.ts',
      'src/features/detail/navigation.ts',
    ],
  },
  'src/features/discover/platform-legacy.json': {
    ui: ['src/features/discover/ui/DiscoverScreen.tsx'],
    logic: [
      'src/features/discover/hooks/useImpressionLogger.ts',
      'src/features/discover/hooks/useResultTap.ts',
    ],
  },
  'src/features/library/platform-legacy.json': {
    ui: [],
    logic: [
      'src/features/library/goBackOrToLibrary.ts',
      'src/features/library/hooks/dropVanishedTrack.ts',
      'src/features/library/hooks/useDeleteTracks.ts',
      'src/features/library/hooks/useExploreArtist.ts',
      'src/features/library/hooks/useLibraryNavigation.ts',
      'src/features/library/hooks/usePlaylistDelete.ts',
      'src/features/library/libraryFailureAlert.ts',
      'src/features/library/pinBatchSummary.ts',
    ],
  },
  'src/features/playback/platform-legacy.json': {
    ui: [],
    logic: [
      'src/features/playback/audioCache.ts',
      'src/features/playback/audioPrefetch.ts',
      'src/features/playback/createNativePlaybackActions.ts',
      'src/features/playback/hooks/PlaybackProvider.tsx',
      'src/features/playback/hooks/trackPlayerProvider.tsx',
      'src/features/playback/hooks/useAppStateChange.ts',
      'src/features/playback/hooks/useIsForeground.ts',
      'src/features/playback/hooks/usePlaybackPosition.ts',
      'src/features/playback/hooks/usePlaybackSignals.ts',
      'src/features/playback/hooks/useQueueResume.ts',
      'src/features/playback/initPlayer.ts',
      'src/features/playback/loadNativeTrack.ts',
      'src/features/playback/nativeTrack.ts',
      'src/features/playback/nativeTrackSwap.ts',
      'src/features/playback/playbackHealth.ts',
      'src/features/playback/playsThroughTrackPlayer.ts',
      'src/features/playback/registerPlaybackService.ts',
      'src/features/playback/seekControls.ts',
      'src/features/playback/service.ts',
    ],
  },
  'src/features/settings/platform-legacy.json': {
    ui: ['src/features/settings/ui/SettingsModal.tsx'],
    logic: ['src/features/settings/reportDiagnostics.ts'],
  },
};

const BLOCK_NAME = {
  ui: 'noInlinePlatformBranchesInFeatureUi',
  logic: 'featureLogicStaysPlatformFree',
};
const RULE_ID = { ui: 'no-restricted-syntax', logic: 'no-restricted-imports' };

const configBlocks: Linter.Config[] = require('../../eslint.config.js');

function baselineFiles(): string[] {
  const featuresDir = path.join(CWD, 'src', 'features');
  const featureFiles = fs
    .readdirSync(featuresDir)
    .map((feature) => `src/features/${feature}/platform-legacy.json`);
  return ['src/app/platform-legacy.json', ...featureFiles].filter((file) =>
    fs.existsSync(path.join(CWD, file)),
  );
}

function readBaseline(file: string): Baseline {
  const contents = JSON.parse(fs.readFileSync(path.join(CWD, file), 'utf8'));
  return { ui: contents.ui ?? [], logic: contents.logic ?? [] };
}

function stillViolates(kind: 'ui' | 'logic', legacyPath: string): boolean {
  const block = configBlocks.find((candidate) => candidate.name === BLOCK_NAME[kind]);
  const linter = new Linter({ configType: 'flat' });
  const messages = linter.verify(
    fs.readFileSync(path.join(CWD, legacyPath), 'utf8'),
    [
      {
        files: ['**/*.{ts,tsx}'],
        languageOptions: { parser: require('@typescript-eslint/parser') },
        rules: block?.rules,
      },
    ],
    { filename: legacyPath },
  );
  return messages.some((message) => message.ruleId === RULE_ID[kind]);
}

describe('platform-legacy baselines only shrink', () => {
  it.each(baselineFiles())('%s lists no new path and no fixed file', (file) => {
    const frozen = FROZEN_BASELINE[file] ?? { ui: [], logic: [] };
    const current = readBaseline(file);

    for (const kind of ['ui', 'logic'] as const) {
      for (const legacyPath of current[kind]) {
        expect({ file, kind, legacyPath, frozen: frozen[kind].includes(legacyPath) }).toEqual({
          file,
          kind,
          legacyPath,
          frozen: true,
        });
        if (!stillViolates(kind, legacyPath)) {
          throw new Error(
            `${legacyPath} no longer violates the ${kind} rule: delete its line from ${file}`,
          );
        }
      }
    }
  });

  it('holds a frozen copy for every baseline file on disk', () => {
    expect(baselineFiles().sort()).toEqual(Object.keys(FROZEN_BASELINE).sort());
  });
});
