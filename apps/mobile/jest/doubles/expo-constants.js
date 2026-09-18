// jest-expo boots with an empty manifest, so `Constants.expoConfig` reads as `{}`
// where a real build carries the app config. Code derived from that config — the
// auth deep-link scheme (#1649) — has to meet in tests what it meets on device,
// so serve app.json's own `expo` block. Everything else stays the real module,
// reached through the prototype so its getters stay lazy.
const actual = jest.requireActual('expo-constants');
const { expo: expoConfig } = require('../../app.json');

const constantsWithTheProjectsAppConfig = Object.create(actual.default, {
  expoConfig: { value: expoConfig, enumerable: true },
});

module.exports = {
  ...actual,
  __esModule: true,
  default: constantsWithTheProjectsAppConfig,
};
