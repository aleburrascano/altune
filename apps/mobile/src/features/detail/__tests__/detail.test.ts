jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/detail public surface', () => {
  it('re-exports the same DetailScreen as the ui module', () => {
    expect(require('@features/detail').DetailScreen).toBe(
      require('@features/detail/ui/DetailScreen').DetailScreen,
    );
  });

  it('exports exactly DetailScreen', () => {
    expect(Object.keys(require('@features/detail'))).toEqual(['DetailScreen']);
  });

  it('does not expose TrackDetailBody', () => {
    expect(require('@features/detail').TrackDetailBody).toBeUndefined();
  });
});
