---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Expo SDK packages

## expo-file-system (OOP API — default since SDK 54)

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

## expo-sqlite (vector search, WASM, tagged templates)

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

## expo-secure-store

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

## expo-audio (replaces expo-av — removed in SDK 55)

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

## expo-video (replaces expo-av video)

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

## expo-image

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

## expo-camera

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

## expo-location

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

## expo-notifications

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

## expo-background-task (replaces expo-background-fetch)

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

## Other device and service packages

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

## New and experimental packages

- **expo-glass-effect** (SDK 54+): `<GlassView>` / `<GlassContainer>` for iOS 26+ Liquid Glass; falls back to regular View on older iOS / Android
- **expo-maps** (Beta, SDK 53+): native maps via `<MapView>`, `<Marker>`, `<Polyline>` — for production prefer `react-native-maps` until stable
- **expo-ui** (Experimental, SDK 53+): native SwiftUI/Compose primitives (`Switch`, `Slider`, `Picker`, `ContextMenu`) — API will change
- **expo-widgets** (Alpha, SDK 55+): iOS home screen widgets — very early, API unstable
- **expo-brownfield** (SDK 55+): integrate Expo into existing native apps
- **expo-server** (SDK 55+): powers Expo Router API routes when `web.output = 'server'`
- **CSS gradients**: `experimental_backgroundImage: 'linear-gradient(...)'` — requires New Architecture

## Deprecated / removed packages

- **expo-av**: REMOVED in SDK 55 — use `expo-audio` + `expo-video`
- **expo-background-fetch**: deprecated — use `expo-background-task`
- **expo-video-thumbnails**: removed — use `expo-video`'s `generateThumbnailsAsync`
- **expo-navigation-bar**: deprecated — most methods are no-ops on Android 16+ (mandatory edge-to-edge); use `react-native-edge-to-edge` `SystemBars` + `useSafeAreaInsets`
- **expo-status-bar**: `translucent` and `backgroundColor` props are no-ops on Android 16+; `style` still works
