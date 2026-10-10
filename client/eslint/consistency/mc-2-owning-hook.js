const OWNERS = {
  'src/shared/searchHistory/useClearSearchHistory.ts': { discovery: ['clearSearchHistory'] },
  'src/shared/playlists/adapters/http/httpPlaylistsPort.ts': {
    playlists: [
      'createPlaylist',
      'renamePlaylist',
      'deletePlaylist',
      'addTracksToPlaylist',
      'removeTracksFromPlaylist',
    ],
  },
  'src/features/discovery/adapters/http/httpFavoritesPort.ts': {
    favorites: ['addFavorite', 'removeFavorite'],
  },
  'src/shared/acquisition/adapters/http/httpAcquisitionPort.ts': { tracks: ['retryAcquisition'] },
  'src/features/library/adapters/http/httpLibraryPort.ts': {
    tracks: ['deleteTrack', 'reacquireTrack'],
  },
  'src/features/detail/adapters/http/httpDetailPort.ts': { tracks: ['createTrack'] },
  'src/features/playback/adapters/http/httpPlaybackPort.ts': {
    audio: ['recoverAudio'],
    playback: ['saveQueueState'],
  },
  'src/features/settings/adapters/http/httpSettingsPort.ts': {
    tracks: ['backfillFeaturedArtists'],
  },
};

const EXEMPT = {
  fetchAudioUrls: 'a read in POST form, several importers',
};

const mutatingByModule = Object.values(OWNERS).reduce((all, owned) => {
  for (const [module, names] of Object.entries(owned)) {
    all[module] = [...new Set([...(all[module] ?? []), ...names])];
  }
  return all;
}, {});

function restrictedPatterns(allowed) {
  return Object.entries(mutatingByModule).flatMap(([module, names]) => {
    const banned = names.filter((name) => !(allowed[module] ?? []).includes(name));
    if (banned.length === 0) return [];
    return [
      {
        group: [`**/apiClient/${module}`],
        importNames: banned,
        allowTypeImports: true,
        message: `Call this apiClient mutation only from its owning hook (MC-2).`,
      },
    ];
  });
}

function importRule(allowed) {
  return { 'no-restricted-imports': ['error', { patterns: restrictedPatterns(allowed) }] };
}

module.exports = {
  id: 'MC-2',
  description: 'Each mutating apiClient function is imported only by its one owning hook',
  exempt: EXEMPT,
  config: [
    {
      ignores: [...Object.keys(OWNERS), 'src/shared/apiClient/**', '**/__tests__/**'],
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
      "import { deleteTrack } from '@shared/apiClient/tracks';\nexport const d = deleteTrack;\n",
      "import { createTrack } from '../../shared/apiClient/tracks';\nexport const c = createTrack;\n",
    ],
    valid: [
      "import { getTracks } from '@shared/apiClient/tracks';\nexport const g = getTracks;\n",
      "import { fetchAudioUrls } from '@shared/apiClient/audio';\nexport const f = fetchAudioUrls;\n",
    ],
  },
};
