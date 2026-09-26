// A seam carved out of PlaylistDetailScreen (#781): the playlist rename. Each test
// pins the behavior the screen had inline before.

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

describe('usePlaylistRename', () => {
  it('does nothing when the playlist is not loaded', () => {
    const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, undefined));
    act(() => result.current.startEditing());
    expect(result.current.isEditing).toBe(false);
  });

  it('seeds the edit name and renames with the trimmed value, closing on settle', () => {
    const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, 'Old'));
    act(() => result.current.startEditing());
    expect(result.current).toMatchObject({ isEditing: true, editName: 'Old' });

    act(() => result.current.setEditName('  New  '));
    act(() => result.current.confirmRename());
    expect(mockRename).toHaveBeenCalledWith('New', expect.any(Object));
    expect(result.current.isEditing).toBe(true);

    act(() => mockRename.mock.calls[0][1].onSettled());
    expect(result.current.isEditing).toBe(false);
  });

  it.each([['   '], ['Old'], [' Old ']])('closes without renaming for %j', (name) => {
    const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, 'Old'));
    act(() => result.current.startEditing());
    act(() => result.current.setEditName(name));
    act(() => result.current.confirmRename());
    expect(mockRename).not.toHaveBeenCalled();
    expect(result.current.isEditing).toBe(false);
  });
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
