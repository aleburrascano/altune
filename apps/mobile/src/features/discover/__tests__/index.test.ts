jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/discover public surface', () => {
  it('exports exactly DiscoverScreen', () => {
    expect(Object.keys(require('@features/discover')).sort()).toEqual(['DiscoverScreen']);
  });
});
