import { act, renderHook } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';

import { usePlaylistRename } from '../hooks/usePlaylistRename';

const mockRename = jest.fn();
jest.mock('@shared/playlists', () => ({
  useRenamePlaylist: () => ({ mutate: mockRename }),
}));

const PLAYLIST_ID = asPlaylistId('pl1');
const CURRENT_NAME = 'Old';

function editingWith(nextName: string) {
  const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, CURRENT_NAME));
  act(() => result.current.startEditing());
  act(() => result.current.setEditName(nextName));
  return result;
}

function settleRename(callIndex: number): void {
  act(() => mockRename.mock.calls[callIndex][1].onSettled());
}

beforeEach(() => {
  jest.clearAllMocks();
});

// Return on a single-line TextInput blurs it, so PlaylistHero's onSubmitEditing and
// onBlur both call confirmRename in one tick (#1698). Two requests for the same name
// race, and a late failure from the first reverts the name the second just committed.
describe('usePlaylistRename — one rename per gesture', () => {
  it('sends a single rename when Return and the blur it triggers confirm in the same tick', () => {
    const result = editingWith('New');

    act(() => {
      result.current.confirmRename();
      result.current.confirmRename();
    });

    expect(mockRename).toHaveBeenCalledTimes(1);
    expect(mockRename).toHaveBeenCalledWith('New', expect.any(Object));
  });

  it('keeps the field open on the dropped confirm, so settling still closes it', () => {
    const result = editingWith('New');

    act(() => {
      result.current.confirmRename();
      result.current.confirmRename();
    });

    expect(result.current.isEditing).toBe(true);
    settleRename(0);
    expect(result.current.isEditing).toBe(false);
  });

  it('renames again once the in-flight rename settles', () => {
    const result = editingWith('New');
    act(() => result.current.confirmRename());
    settleRename(0);

    act(() => result.current.startEditing());
    act(() => result.current.setEditName('Newer'));
    act(() => result.current.confirmRename());

    expect(mockRename).toHaveBeenCalledTimes(2);
    expect(mockRename).toHaveBeenLastCalledWith('Newer', expect.any(Object));
  });
});
