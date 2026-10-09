jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/library public surface', () => {
  it('exports exactly the three route screens', () => {
    expect(Object.keys(require('@features/library')).sort()).toEqual([
      'FeaturingScreen',
      'LibraryScreen',
      'PlaylistDetailScreen',
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
});
