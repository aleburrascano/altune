process.env.EXPO_PUBLIC_SUPABASE_URL =
  process.env.EXPO_PUBLIC_SUPABASE_URL || 'https://fixture.supabase.co';
process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY =
  process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY || 'fixture-anon-key';

function mockNativeViewAs(moduleName, mockExportedNames) {
  jest.mock(moduleName, () => {
    const { View } = require('react-native');
    const stub = { __esModule: true, default: View };
    for (const name of mockExportedNames) stub[name] = View;
    return stub;
  });
}

jest.mock('expo-web-browser', () => ({
  maybeCompleteAuthSession: jest.fn(),
  openAuthSessionAsync: jest.fn().mockResolvedValue({ type: 'cancel' }),
}));

jest.mock('expo-file-system', () => require('./doubles/expo-file-system.js'));
jest.mock('expo-secure-store', () => require('./doubles/expo-secure-store.js'));
jest.mock('react-native-track-player', () => require('./doubles/react-native-track-player.js'));

mockNativeViewAs('expo-blur', ['BlurView']);
mockNativeViewAs('expo-linear-gradient', ['LinearGradient']);

const mockEverySvgPrimitiveLucideEnumeratesAtImport = [
  'Svg',
  'Circle',
  'Ellipse',
  'G',
  'Text',
  'TSpan',
  'TextPath',
  'Path',
  'Polygon',
  'Polyline',
  'Line',
  'Rect',
  'Use',
  'Image',
  'Symbol',
  'Defs',
  'LinearGradient',
  'RadialGradient',
  'Stop',
  'ClipPath',
  'Pattern',
  'Mask',
  'Marker',
  'ForeignObject',
];

mockNativeViewAs('react-native-svg', mockEverySvgPrimitiveLucideEnumeratesAtImport);
