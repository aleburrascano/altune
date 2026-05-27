// Jest stand-in for Metro's `expo/virtual/env` virtual module. babel-preset-expo
// rewrites `process.env.EXPO_PUBLIC_*` reads to `require('expo/virtual/env').env.*`
// at bundle time. In jest, expose `env` as a named export pointing at the real
// process.env so tests can inject vars normally.
module.exports = { env: process.env };
