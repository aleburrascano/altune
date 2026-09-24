import { act, renderHook } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useAlbumDetailState } from '../hooks/useAlbumDetailState';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

const mockSave = {
  mutate: jest.fn(),
  mutateAsync: jest.fn(() => Promise.resolve()),
  isPending: false,
};
let mockComplete = false;

const oneTrack = {
  kind: 'track',
  title: 'A',
  subtitle: 'Artist',
  image_url: null,
  confidence: 'high',
  sources: [],
  extras: {},
};

jest.mock('../hooks/useSaveTrack', () => ({ useSaveTrack: () => mockSave }));
jest.mock('../hooks/useAlbumTracks', () => ({
  useAlbumTracks: () => ({
    tracks: [oneTrack],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  }),
}));
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracksForAlbum: () => Object.assign([], { complete: mockComplete }),
}));
jest.mock('../hooks/useAlbumDiscovery', () => ({
  useAlbumDiscovery: () => ({
    tracks: [],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  }),
}));
jest.mock('../hooks/useOwnedPlayback', () => ({
  useOwnedPlayback: () => ({
    owned: { playable: [], unownedCount: 1, acquiringCount: 0 },
    playButton: { label: 'Play', disabled: true },
    onPlayOwned: jest.fn(),
    ownedFor: () => null,
    onQuickSave: jest.fn(),
  }),
}));

const album: DiscoveryResult = {
  kind: 'album',
  title: 'Album',
  subtitle: 'Artist',
  image_url: null,
  confidence: 'high',
  sources: [{ provider: 'deezer', external_id: 'a', url: 'https://deezer/a' }],
  extras: {},
};

beforeEach(() => mockSave.mutateAsync.mockClear());

describe('onSaveAll with a partial library lookup', () => {
  it('does not re-save when ownership is unknown', () => {
    mockComplete = false;
    const hook = renderHook(() => useAlbumDetailState(album, '/discover/detail'));
    act(() => hook.result.current.onSaveAll());
    expect(mockSave.mutateAsync).not.toHaveBeenCalled();
  });

  it('saves the unowned tracks once the lookup is complete', () => {
    mockComplete = true;
    const hook = renderHook(() => useAlbumDetailState(album, '/discover/detail'));
    act(() => hook.result.current.onSaveAll());
    expect(mockSave.mutateAsync).toHaveBeenCalledTimes(1);
  });
});
