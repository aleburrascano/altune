export default {
  rules: {
    // Banner wraps string children in <Text> internally (Banner.tsx:49)
    'react-doctor/rn-no-raw-text': 'off',
    // RN Animated used intentionally per ADR-0008 (no TurboModule in Expo Go)
    'react-doctor/rn-prefer-reanimated': 'off',
    // react-doctor and fallow are CLI tools, not imported in code
    'deslop/unused-dev-dependency': 'off',
  },
};
