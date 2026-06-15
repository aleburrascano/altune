---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Expo configuration — app.config.ts, config plugins, SDK preferences, EAS

## App config

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

## Config plugins

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

## SDK module preferences

Prefer Expo SDK packages over community alternatives:

| Prefer | Over |
|---|---|
| `expo-image` | `react-native-fast-image` |
| `expo-file-system` | `react-native-fs` |
| `expo-camera` | `react-native-camera` |
| `expo-notifications` | `react-native-push-notification` |
| `expo-secure-store` | `react-native-keychain` |

## Module resolution and routing

- Use `expo-modules-core` for native module foundations
- Use `expo-constants` for app metadata and environment info
- Use `expo-updates` for OTA update management
- Expo Router: file-based routing exclusively, with typed routes

## EAS build and deploy

### EAS Update

- Channel-based deployment: `dev`, `preview`, `production`
- Always test updates on `preview` channel before promoting to `production`
- Use `runtimeVersion` policy to prevent incompatible updates from being applied
- `expo-updates` `useUpdates()` hook for in-app update state management

### Expo Modules API

- Prefer Expo Modules API over bare Turbo Modules for custom native code
- Use `expo-modules-core` as the foundation for native module development
