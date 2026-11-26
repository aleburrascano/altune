# jest setup

Shims in `jest.config.js` (`moduleNameMapper`) are only for native modules jest cannot load, never to steer behavior under test. Prefer the library's own published jest mock; a hand-written double is kept only where that mock cannot load or be mapped (safe-area-context: its mock requires the package it replaces; reanimated: its mock loads the native worklets runtime, which fails under jest; the swipeable has none).
