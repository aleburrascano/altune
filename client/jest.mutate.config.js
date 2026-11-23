const { projects } = require('./jest.config');

const projectName = process.env.CLIENT_MUTATE_JEST_PROJECT;
const project = projects.find(({ displayName }) => displayName === projectName);
if (!project) {
  throw new Error(
    `jest.mutate.config.js: CLIENT_MUTATE_JEST_PROJECT is '${projectName}', and must name one of the jest projects: ${projects.map(({ displayName }) => displayName).join(', ')}. scripts/ci/client-mutate.sh sets it.`,
  );
}

const testRoots = (process.env.CLIENT_MUTATE_TEST_ROOTS ?? '').split(':').filter(Boolean);
if (testRoots.length === 0) {
  throw new Error(
    'jest.mutate.config.js: CLIENT_MUTATE_TEST_ROOTS is empty, and must list the folders whose tests may kill a mutant, joined by ":". scripts/ci/client-mutate.sh sets it.',
  );
}

module.exports = { ...project, roots: testRoots.map((root) => `<rootDir>/${root}`) };
