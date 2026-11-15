jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/detail public surface', () => {
  it('re-exports the same DetailScreen as the ui module', () => {
    expect(require('@features/detail').DetailScreen).toBe(
      require('@features/detail/ui/DetailScreen').DetailScreen,
    );
  });

  it('exports exactly the screens, the unreleased search hits and the port the app wires', () => {
    expect(Object.keys(require('@features/detail')).sort()).toEqual([
      'DetailPortProvider',
      'DetailScreen',
      'UnreleasedAllScreen',
      'UnreleasedSearchHits',
      'httpDetailPort',
    ]);
  });

  it('does not expose TrackDetailBody', () => {
    expect(require('@features/detail').TrackDetailBody).toBeUndefined();
  });
});
