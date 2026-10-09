jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/discovery public surface', () => {
  it('exports exactly DiscoverScreen', () => {
    expect(Object.keys(require('@features/discovery')).sort()).toEqual(['DiscoverScreen']);
  });
});
