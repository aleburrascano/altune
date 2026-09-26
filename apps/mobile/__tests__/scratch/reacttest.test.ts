const outer = require('react');
test('same react instance inside isolateModules', () => {
  let inner: unknown;
  jest.isolateModules(() => {
    inner = require('react');
  });
  expect(inner).toBe(outer);
});
