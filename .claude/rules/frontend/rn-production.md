---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native production: security, accessibility, Expo SDK, deployment

## Security

### Secrets management

- NEVER hardcode API keys, tokens, or secrets in source code
- Use `expo-secure-store` (not AsyncStorage) for sensitive tokens
- `.env` files excluded from git; `.env.example` checked in as documentation
- Use EAS Secrets for CI/CD environment variables
- Runtime secrets delivered via backend API, never bundled in the client

```tsx
// GOOD — secure storage for tokens
import * as SecureStore from 'expo-secure-store';

await SecureStore.setItemAsync('auth_token', token);
const token = await SecureStore.getItemAsync('auth_token');

// BAD — AsyncStorage is not encrypted
import AsyncStorage from '@react-native-async-storage/async-storage';
await AsyncStorage.setItem('auth_token', token); // NEVER do this
```

### Deep linking

- Validate ALL incoming URLs before processing
- Whitelist allowed hosts and paths explicitly
- Sanitize URL parameters — never pass raw params to sensitive operations
- Never construct navigation routes from unvalidated external input

### Network

- HTTPS only for all network requests
- Certificate pinning for critical endpoints (auth, payments)
- Timeout all requests (15s default) — never leave requests open-ended
- Handle offline state gracefully; queue or reject operations explicitly

### WebView

- Set `originWhitelist` explicitly — never use `['*']`
- Disable JavaScript in WebViews that don't need it
- Use `onShouldStartLoadWithRequest` to intercept and validate navigation

### Input validation

- Sanitize all user input against XSS before rendering
- Validate on both client AND server — client validation is UX, server validation is security
- Use parameterized queries for any local database operations
- Limit input lengths at the component level

### Data storage

- Sensitive data (tokens, credentials, PII): `expo-secure-store`
- Non-sensitive preferences: `AsyncStorage`
- Never store PII in logs or crash reports
- Clear sensitive storage on logout

## Accessibility

### Labels

Every interactive element MUST have accessibility metadata:

```tsx
// GOOD — full accessibility props
<Pressable
  onPress={onPlay}
  accessibilityLabel="Play track"
  accessibilityRole="button"
  accessibilityHint="Starts playing the current track"
>
  <PlayIcon />
</Pressable>

// BAD — bare pressable with no accessibility info
<Pressable onPress={onPlay}>
  <PlayIcon />
</Pressable>
```

### Touch targets

- 44x44pt minimum for all interactive elements
- Use `hitSlop` to expand small visual elements to meet the minimum
- 8pt minimum spacing between adjacent touch targets

### Screen reader

- Test with VoiceOver (iOS) and TalkBack (Android) on real devices
- Ensure logical focus order — tab order should match visual reading order
- Hide purely decorative elements with `accessibilityElementsHidden` or `importantForAccessibility="no"`
- Announce dynamic content changes with `AccessibilityInfo.announceForAccessibility`

### Color and contrast

- 4.5:1 contrast ratio minimum (WCAG AA)
- Never rely on color alone to convey information
- Support both light and dark mode
- Test with the "Increase Contrast" accessibility setting enabled

### State communication

```tsx
// GOOD — full accessibility state on a toggle
<Switch
  value={isEnabled}
  onValueChange={setIsEnabled}
  accessibilityLabel="Enable notifications"
  accessibilityRole="switch"
  accessibilityState={{ checked: isEnabled }}
/>
```

- Use `accessibilityState` for disabled, selected, checked, expanded states
- Use `accessibilityValue` for sliders, progress bars, and numeric inputs
- Mark required form fields explicitly

## Expo configuration

### App config

- Use `app.config.ts` (dynamic) over static `app.json`
- Split configuration by environment (dev/preview/production)
- Keep secrets out of app config — use `process.env.EXPO_PUBLIC_*` or EAS Secrets

```tsx
// app.config.ts
import { ExpoConfig, ConfigContext } from 'expo/config';

export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: process.env.EXPO_PUBLIC_APP_NAME ?? 'Altune',
  slug: 'altune',
  version: '1.0.0',
  scheme: 'altune',
  orientation: 'portrait',
  icon: './assets/icon.png',
  splash: {
    image: './assets/splash.png',
    resizeMode: 'contain',
    backgroundColor: '#111827',
  },
  ios: {
    supportsTablet: true,
    bundleIdentifier: 'com.altune.app',
  },
  android: {
    adaptiveIcon: {
      foregroundImage: './assets/adaptive-icon.png',
      backgroundColor: '#111827',
    },
    package: 'com.altune.app',
  },
  plugins: [],
  extra: {
    eas: { projectId: process.env.EAS_PROJECT_ID },
  },
});
```

### Config plugins

- Use `plugins/` directory for custom config plugins
- Use `withInfoPlist` / `withAndroidManifest` for native config
- Test all plugin changes with `npx expo prebuild --clean`
- NEVER modify `ios/` or `android/` directories directly

```tsx
// plugins/withCustomConfig.ts
import { ConfigPlugin, withInfoPlist, withAndroidManifest } from 'expo/config-plugins';

const withCustomConfig: ConfigPlugin = (config) => {
  config = withInfoPlist(config, (config) => {
    config.modResults.NSCameraUsageDescription = 'Camera access for scanning';
    return config;
  });

  config = withAndroidManifest(config, (config) => {
    const mainApp = config.modResults.manifest.application?.[0];
    if (mainApp) {
      mainApp.$['android:usesCleartextTraffic'] = 'false';
    }
    return config;
  });

  return config;
};

export default withCustomConfig;
```

### SDK module preferences

Prefer Expo SDK packages over community alternatives:

| Prefer | Over |
|---|---|
| `expo-image` | `react-native-fast-image` |
| `expo-file-system` | `react-native-fs` |
| `expo-camera` | `react-native-camera` |
| `expo-notifications` | `react-native-push-notification` |
| `expo-secure-store` | `react-native-keychain` |

### Module resolution and routing

- Use `expo-modules-core` for native module foundations
- Use `expo-constants` for app metadata and environment info
- Use `expo-updates` for OTA update management
- Expo Router: file-based routing exclusively, with typed routes

## Expo SDK packages

### expo-file-system (OOP API — default since SDK 54)

```tsx
import { File, Directory, Paths } from 'expo-file-system';

const file = new File(Paths.document, 'notes.txt');
file.create();
file.write('Hello world');
const content = file.text();
file.delete();

const dir = new Directory(Paths.document, 'uploads');
dir.create();
const items = dir.list(); // returns (File | Directory)[]
```

- `Paths.document` — persistent document directory
- `Paths.cache` — cache directory
- Legacy `FileSystem.*` function API is at `expo-file-system/legacy`
- Use `file.info()` for metadata (size, dates)
- Use `file.base64()` for binary reads

### expo-sqlite (vector search, WASM, tagged templates)

```tsx
import * as SQLite from 'expo-sqlite';
import { SQLiteProvider, useSQLiteContext } from 'expo-sqlite';

const db = await SQLite.openDatabaseAsync('app.db');
await db.execAsync('CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT)');
const result = await db.runAsync('INSERT INTO users (name) VALUES (?)', 'Alice');
const user = await db.getFirstAsync('SELECT * FROM users WHERE id = ?', 1);

// Tagged template API
const sql = db.sql;
const users = await sql<User>`SELECT * FROM users WHERE age > ${21}`;
const single = await sql<User>`SELECT * FROM users WHERE id = ${1}`.first();

// Transactions
await db.withTransactionAsync(async () => {
  await db.runAsync('INSERT INTO users (name) VALUES (?)', 'Bob');
  await db.runAsync('INSERT INTO users (name) VALUES (?)', 'Charlie');
});

// React integration
<SQLiteProvider databaseName="app.db" onInit={migrateDb}>
  <App />
</SQLiteProvider>
```

- Vector search via bundled `sqlite-vec` extension
- Web/WASM support with metro config for COOP/COEP headers
- Use `db.withTransactionAsync` for atomic multi-statement operations
- Use `db.getEachAsync` for cursor-based async iteration over large result sets

### expo-secure-store

```tsx
import * as SecureStore from 'expo-secure-store';

// Async API
await SecureStore.setItemAsync('auth_token', token);
const token = await SecureStore.getItemAsync('auth_token');
await SecureStore.deleteItemAsync('auth_token');

// Synchronous API
SecureStore.setItem('session_id', id);
const session = SecureStore.getItem('session_id');

// Biometric authentication
if (SecureStore.canUseBiometricAuthentication()) {
  await SecureStore.setItemAsync('secret', value, {
    requireAuthentication: true,
    authenticationPrompt: 'Verify identity',
  });
}

// iOS Keychain accessibility
await SecureStore.setItemAsync('offline_token', token, {
  keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK,
  keychainService: 'com.myapp.api',
});
```

- Keys: alphanumeric + `.` `-` `_` only
- Values: strings only (use `JSON.stringify` for objects)
- Size limit: ~2KB per value on iOS Keychain
- `requireAuthentication`: Android prompts on ALL ops; iOS only on read/update
- Not supported in Expo Go with biometric auth — use dev builds

### expo-audio (replaces expo-av — removed in SDK 55)

```tsx
import { useAudioPlayer } from 'expo-audio';
import { useEvent } from 'expo';

const player = useAudioPlayer(require('./sound.mp3'), {
  updateInterval: 500,
  downloadFirst: false,
});

const { isPlaying } = useEvent(player, 'playingChange', {
  isPlaying: player.playing,
});

player.play();
player.pause();
player.seekTo(seconds); // NOTE: seconds, not milliseconds
player.replace(newSource);
player.remove();
player.setPlaybackRate(1.5);
player.volume = 0.8;
player.loop = true;

// Lock screen controls
player.setActiveForLockScreen(true, {
  title: 'Song Name',
  artist: 'Artist',
  album: 'Album',
  artworkSource: require('./cover.png'),
});
```

Recording:

```tsx
import { useAudioRecorder, useAudioRecorderState, AudioModule, RecordingPresets, setAudioModeAsync } from 'expo-audio';

await AudioModule.requestRecordingPermissionsAsync();
await setAudioModeAsync({ playsInSilentMode: true, allowsRecording: true });

const recorder = useAudioRecorder(RecordingPresets.HIGH_QUALITY);
recorder.record();
await recorder.stop();
console.log(recorder.uri);
```

Migration from expo-av:

| expo-av | expo-audio |
|---|---|
| `Audio.Sound.createAsync(source)` | `useAudioPlayer(source)` |
| `sound.playAsync()` | `player.play()` |
| `sound.setPositionMillis(ms)` | `player.seekTo(seconds)` — seconds not ms |
| `sound.unloadAsync()` | `player.remove()` |
| `Audio.Recording` class | `useAudioRecorder(preset)` |

### expo-video (replaces expo-av video)

```tsx
import { useVideoPlayer, VideoView } from 'expo-video';
import { useEvent } from 'expo';

const player = useVideoPlayer('https://example.com/video.mp4', (p) => {
  p.loop = true;
  p.play();
});

<VideoView
  player={player}
  style={{ width: 350, height: 275 }}
  contentFit="cover"
  nativeControls={true}
  allowsPictureInPicture={true}
  allowsFullscreen={true}
/>
```

- DRM support via `drm` option on player source (`widevine`, `fairplay`, `clearkey`)
- Picture-in-Picture requires config plugin: `["expo-video", { "supportsPictureInPicture": true }]`
- Events via `useEvent` from `'expo'`: `playingChange`, `statusChange`, `timeUpdate`, `playToEnd`

### expo-image

```tsx
import { Image } from 'expo-image';

<Image
  source="https://example.com/photo.jpg"
  placeholder={{ blurhash: '|rF?hV%2WCj[ayj[a|j[az' }}
  contentFit="cover"
  transition={300}
  cachePolicy="memory-disk"
  priority="high"
  recyclingKey={uri}
  defaultSource={fallbackImage}
  accessibilityLabel="Photo"
  style={{ width: 200, height: 200 }}
/>

// Static methods
Image.prefetch(urls);
Image.clearDiskCache();
Image.clearMemoryCache();
Image.generateBlurhashAsync(url, [4, 3]);
```

- SF Symbols via `source={{ uri: 'sf:house.fill' }}` (iOS)
- Prefer `expo-image` with SF Symbols over `expo-symbols` — same result, one less package
- Use `recyclingKey` when rendering in FlashList to reset on cell recycle

### expo-camera

```tsx
import { CameraView, useCameraPermissions } from 'expo-camera';

const [permission, requestPermission] = useCameraPermissions();

<CameraView
  ref={cameraRef}
  style={{ flex: 1 }}
  facing="back"
  flash="auto"
  zoom={0}
  barcodeScannerSettings={{ barcodeTypes: ['qr', 'ean13'] }}
  onBarcodeScanned={(result) => console.log(result.data)}
/>

// Ref methods
const photo = await cameraRef.current.takePictureAsync({ quality: 0.8 });
const video = await cameraRef.current.recordAsync();
cameraRef.current.stopRecording();
```

- Always check and request permissions before rendering CameraView
- Use `useMicrophonePermissions` separately for video recording with audio

### expo-location

```tsx
import * as Location from 'expo-location';

await Location.requestForegroundPermissionsAsync();

const location = await Location.getCurrentPositionAsync({
  accuracy: Location.Accuracy.High,
});

// Watch position
const subscription = await Location.watchPositionAsync(
  { accuracy: Location.Accuracy.High, distanceInterval: 10, timeInterval: 5000 },
  (location) => console.log(location.coords),
);
subscription.remove();

// Geocoding
const coords = await Location.geocodeAsync('1600 Amphitheatre Parkway');
const address = await Location.reverseGeocodeAsync({ latitude: 37.422, longitude: -122.084 });
```

- Background location requires `expo-task-manager` + `TaskManager.defineTask` in global scope
- Android 11+: background permission opens system settings, not in-app dialog
- Accuracy levels: `Lowest` (~3000m), `Low` (~1000m), `Balanced` (~100m), `High` (~10m), `Highest` (~1m)

### expo-notifications

```tsx
import * as Notifications from 'expo-notifications';

// Handler (use shouldShowBanner/shouldShowList, NOT deprecated shouldShowAlert)
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowBanner: true,
    shouldShowList: true,
    shouldPlaySound: true,
    shouldSetBadge: true,
  }),
});

// Schedule
await Notifications.scheduleNotificationAsync({
  content: { title: 'Reminder', body: 'Drink water' },
  trigger: {
    type: Notifications.SchedulableTriggerInputTypes.TIME_INTERVAL,
    seconds: 60 * 20,
    repeats: true,
  },
});

// Daily
await Notifications.scheduleNotificationAsync({
  content: { title: 'Morning', body: 'Good morning!' },
  trigger: {
    type: Notifications.SchedulableTriggerInputTypes.DAILY,
    hour: 8,
    minute: 0,
  },
});
```

### expo-background-task (replaces expo-background-fetch)

```tsx
import * as BackgroundTask from 'expo-background-task';
import * as TaskManager from 'expo-task-manager';

const TASK_NAME = 'background-sync';

// Define task in GLOBAL SCOPE (outside components)
TaskManager.defineTask(TASK_NAME, async () => {
  try {
    await syncData();
    return BackgroundTask.BackgroundTaskResult.Success;
  } catch {
    return BackgroundTask.BackgroundTaskResult.Failed;
  }
});

await BackgroundTask.registerTaskAsync(TASK_NAME, {
  minimumInterval: 15 * 60,
});

// Dev testing
await BackgroundTask.triggerTaskWorkerForTestingAsync();
```

### Other device and service packages

- **expo-haptics**: `impactAsync` (Light/Medium/Heavy/Rigid/Soft), `notificationAsync` (Success/Warning/Error), `selectionAsync`
- **expo-blur**: `<BlurView intensity={80} tint="dark" />` — Android support in SDK 55 via `BlurTargetView` wrapper
- **expo-constants**: `Constants.expoConfig`, `Constants.executionEnvironment`, `Constants.isDevice` — prefer `process.env.EXPO_PUBLIC_*` over `Constants.expoConfig.extra` for env vars
- **expo-updates**: `useUpdates()` hook for OTA update state; `checkForUpdateAsync` + `fetchUpdateAsync` + `reloadAsync` flow
- **expo-auth-session**: OAuth flows with `useAuthRequest` + `useAutoDiscovery`; call `WebBrowser.maybeCompleteAuthSession()` for web redirect
- **expo-local-authentication**: biometric auth via `authenticateAsync`; check `hasHardwareAsync` + `isEnrolledAsync` first
- **expo-sensors**: `Accelerometer`, `Gyroscope`, `Barometer`, `DeviceMotion`, `Magnetometer`, `Pedometer`, `LightSensor` — all follow `addListener` / `setUpdateInterval` / `isAvailableAsync` pattern
- **expo-clipboard**: `setStringAsync` / `getStringAsync`, image support via `setImageAsync` / `getImageAsync`
- **expo-store-review**: `requestReview()` after `isAvailableAsync()` check
- **expo-crypto**: `digestStringAsync` for hashing, `getRandomBytesAsync`, `randomUUID`; AES-GCM encryption in SDK 55

### New and experimental packages

- **expo-glass-effect** (SDK 54+): `<GlassView>` / `<GlassContainer>` for iOS 26+ Liquid Glass; falls back to regular View on older iOS / Android
- **expo-maps** (Beta, SDK 53+): native maps via `<MapView>`, `<Marker>`, `<Polyline>` — for production prefer `react-native-maps` until stable
- **expo-ui** (Experimental, SDK 53+): native SwiftUI/Compose primitives (`Switch`, `Slider`, `Picker`, `ContextMenu`) — API will change
- **expo-widgets** (Alpha, SDK 55+): iOS home screen widgets — very early, API unstable
- **expo-brownfield** (SDK 55+): integrate Expo into existing native apps
- **expo-server** (SDK 55+): powers Expo Router API routes when `web.output = 'server'`
- **CSS gradients**: `experimental_backgroundImage: 'linear-gradient(...)'` — requires New Architecture

### Deprecated / removed packages

- **expo-av**: REMOVED in SDK 55 — use `expo-audio` + `expo-video`
- **expo-background-fetch**: deprecated — use `expo-background-task`
- **expo-video-thumbnails**: removed — use `expo-video`'s `generateThumbnailsAsync`
- **expo-navigation-bar**: deprecated — most methods are no-ops on Android 16+ (mandatory edge-to-edge); use `react-native-edge-to-edge` `SystemBars` + `useSafeAreaInsets`
- **expo-status-bar**: `translucent` and `backgroundColor` props are no-ops on Android 16+; `style` still works

## EAS build and deploy

### EAS Update

- Channel-based deployment: `dev`, `preview`, `production`
- Always test updates on `preview` channel before promoting to `production`
- Use `runtimeVersion` policy to prevent incompatible updates from being applied
- `expo-updates` `useUpdates()` hook for in-app update state management

### Expo Modules API

- Prefer Expo Modules API over bare Turbo Modules for custom native code
- Use `expo-modules-core` as the foundation for native module development
