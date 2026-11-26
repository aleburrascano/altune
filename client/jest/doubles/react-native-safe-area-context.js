const React = require('react');
const actual = require('react-native-safe-area-context/lib/commonjs/index.js');

const MOCK_INITIAL_METRICS = {
  frame: { width: 320, height: 640, x: 0, y: 0 },
  insets: { left: 0, right: 0, bottom: 0, top: 0 },
};

module.exports = {
  ...actual,
  initialWindowMetrics: MOCK_INITIAL_METRICS,
  useSafeAreaInsets: jest.fn(
    () => React.useContext(actual.SafeAreaInsetsContext) ?? MOCK_INITIAL_METRICS.insets,
  ),
  useSafeAreaFrame: jest.fn(
    () => React.useContext(actual.SafeAreaFrameContext) ?? MOCK_INITIAL_METRICS.frame,
  ),
  SafeAreaProvider: ({ children, initialMetrics }) =>
    React.createElement(
      actual.SafeAreaFrameContext.Provider,
      { value: initialMetrics?.frame ?? MOCK_INITIAL_METRICS.frame },
      React.createElement(
        actual.SafeAreaInsetsContext.Provider,
        { value: initialMetrics?.insets ?? MOCK_INITIAL_METRICS.insets },
        children,
      ),
    ),
};
