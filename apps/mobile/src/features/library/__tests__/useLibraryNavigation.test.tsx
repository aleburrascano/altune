import { renderHook } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';

import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

function makeRouter() {
  return { push: jest.fn() } as unknown as Parameters<typeof useLibraryNavigation>[0];
}

function playlist(id: string): PlaylistResponse {
  return {
    id: asPlaylistId(id),
    name: 'A Playlist',
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

describe('useLibraryNavigation — navigateToPlaylist', () => {
  it('pushes /library/playlist/<id>', () => {
    const router = makeRouter();
    const { result } = renderHook(() => useLibraryNavigation(router));

    result.current.navigateToPlaylist(playlist('p1'));

    expect(router.push).toHaveBeenCalledWith('/library/playlist/p1');
  });
});
