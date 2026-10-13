const config = require('@/../jest.config');
const tsconfig = require('@/../tsconfig.json');

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

  it('resolves every tsconfig path alias in every project', () => {
    const aliases = Object.keys(tsconfig.compilerOptions.paths).map((alias) =>
      alias.replace('/*', '/(.*)'),
    );
    for (const project of config.projects) {
      for (const alias of aliases) {
        expect(project.moduleNameMapper).toHaveProperty([`^${alias}$`]);
      }
    }
  });

  it('exits when the run ends, even with open handles left behind', () => {
    expect(config.forceExit).toBe(true);
  });
});
