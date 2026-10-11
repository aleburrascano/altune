const config = require('../jest.config');

describe('the mobile jest config', () => {
  it('runs tests in random order', () => {
    expect(config.randomize).toBe(true);
  });

  it('restores every mock after each test in every project', () => {
    expect(config.projects).toHaveLength(2);
    for (const project of config.projects) {
      expect(project.restoreMocks).toBe(true);
    }
  });

  it('exits when the run ends, even with open handles left behind', () => {
    expect(config.forceExit).toBe(true);
  });
});
