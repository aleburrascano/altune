const { getConfig } = require('@testing-library/react-native/build/config');

describe('setup-after-env', () => {
  it('raises the RNTL asyncUtilTimeout so waitFor tolerates a loaded machine', () => {
    expect(getConfig().asyncUtilTimeout).toBe(5000);
  });
});
