const OWNERS = {
  'src/shared/search-history/useClearSearchHistory.ts': { discovery: ['clearSearchHistory'] },
  'src/shared/playlists/mutations.ts': {
    playlists: [
      'createPlaylist',
      'renamePlaylist',
      'deletePlaylist',
      'addTracksToPlaylist',
      'removeTracksFromPlaylist',
    ],
  },
  'src/shared/favorites/useFavorites.ts': { favorites: ['addFavorite', 'removeFavorite'] },
  'src/features/settings/hooks/useSubmitReport.ts': { feedback: ['submitReport'] },
  'src/features/detail/hooks/useSaveTrack.ts': { tracks: ['createTrack'] },
  'src/features/library/hooks/useDeleteTracks.ts': { tracks: ['deleteTrack'] },
  'src/shared/acquisition/useRetryAcquisition.ts': { tracks: ['retryAcquisition'] },
  'src/features/settings/hooks/useBackfillFeatured.ts': { tracks: ['backfillFeaturedArtists'] },
  'src/features/library/hooks/useReacquireTrack.ts': { tracks: ['reacquireTrack'] },
  'src/features/playback/native/service.ts': { audio: ['recoverAudio'] },
  'src/features/playback/native/useQueueResume.ts': { playback: ['saveQueueState'] },
};

const EXEMPT = {
  fetchAudioUrls: 'a read in POST form, several importers',
};

const mutatingByModule = Object.values(OWNERS).reduce((all, owned) => {
  for (const [module, names] of Object.entries(owned)) {
    all[module] = [...(all[module] ?? []), ...names];
  }
  return all;
}, {});

function restrictedPatterns(allowed) {
  return Object.entries(mutatingByModule).flatMap(([module, names]) => {
    const banned = names.filter((name) => !(allowed[module] ?? []).includes(name));
    if (banned.length === 0) return [];
    return [
      {
        group: [`**/api-client/${module}`],
        importNames: banned,
        message: `Call this api-client mutation only from its owning hook (MC-2).`,
      },
    ];
  });
}

function importRule(allowed) {
  return { 'no-restricted-imports': ['error', { patterns: restrictedPatterns(allowed) }] };
}

module.exports = {
  id: 'MC-2',
  description: 'Each mutating api-client function is imported only by its one owning hook',
  baseline: 0,
  exempt: EXEMPT,
  config: [
    {
      ignores: [...Object.keys(OWNERS), 'src/shared/api-client/**', '**/__tests__/**'],
      rules: importRule({}),
    },
    ...Object.entries(OWNERS).map(([file, allowed]) => ({
      files: [file],
      ignores: ['**/__tests__/**'],
      rules: importRule(allowed),
    })),
  ],
  examples: {
    invalid: [
      "import { deleteTrack } from '@shared/api-client/tracks';\nexport const d = deleteTrack;\n",
      "import { createTrack } from '../../shared/api-client/tracks';\nexport const c = createTrack;\n",
    ],
    valid: [
      "import { getTracks } from '@shared/api-client/tracks';\nexport const g = getTracks;\n",
      "import { fetchAudioUrls } from '@shared/api-client/audio';\nexport const f = fetchAudioUrls;\n",
    ],
  },
};
