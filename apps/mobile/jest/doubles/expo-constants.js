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
