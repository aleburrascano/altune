jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/library public surface', () => {
  it('exports exactly the three route screens and the chrome sidebar pieces', () => {
    expect(Object.keys(require('@features/library')).sort()).toEqual([
      'FeaturingScreen',
      'LibraryScreen',
      'PlaylistDetailScreen',
      'SidebarPlaylists',
      'usePlaylistActions',
    ]);
  });

  it('re-exports the same LibraryScreen as the ui module', () => {
    expect(require('@features/library').LibraryScreen).toBe(
      require('@features/library/ui/LibraryScreen').LibraryScreen,
    );
  });

  it('does not expose LibraryGrid or listContent', () => {
    const surface = require('@features/library');
    expect(surface.LibraryGrid).toBeUndefined();
    expect(surface.listContent).toBeUndefined();
  });

  it('keeps listContent in its own styles module', () => {
    expect(require('@features/library/ui/listContentStyles').listContent.padded).toEqual({
      paddingBottom: expect.any(Number),
    });
    expect(require('@features/library/ui/LibraryListParts').listContent).toBeUndefined();
  });
});
