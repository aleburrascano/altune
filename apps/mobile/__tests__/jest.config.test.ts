const config = require('../jest.config');

describe('the mobile jest config', () => {
  it('runs tests in random order', () => {
    expect(config.randomize).toBe(true);
  });

  it('restores every mock after each test', () => {
    expect(config.restoreMocks).toBe(true);
  });
});
