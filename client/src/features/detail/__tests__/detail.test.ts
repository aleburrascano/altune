jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/detail public surface', () => {
  it('re-exports the same DetailScreen as the ui module', () => {
    expect(require('@features/detail').DetailScreen).toBe(
      require('@features/detail/ui/DetailScreen').DetailScreen,
    );
  });

  it('exports exactly DetailScreen and the unreleased full-list screen', () => {
    expect(Object.keys(require('@features/detail')).sort()).toEqual([
      'DetailScreen',
      'UnreleasedAllScreen',
    ]);
  });

  it('does not expose TrackDetailBody', () => {
    expect(require('@features/detail').TrackDetailBody).toBeUndefined();
  });
});
