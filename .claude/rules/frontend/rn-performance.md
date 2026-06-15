---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native performance — rendering, lists, images, bundle size, animations, startup

## Rendering

- `React.memo` on list components, `useCallback` for memoized children, `useMemo` for expensive computations
- Never inline functions in JSX loops

## Lists

- FlashList for >20 items (preferred over FlatList)
- `keyExtractor`, `getItemLayout` for fixed heights
- `windowSize` / `maxToRenderPerBatch` / `initialNumToRender`
- `removeClippedSubviews` on Android

## Images

- `expo-image` for all images
- Explicit `width` / `height`, `contentFit="cover"`, `placeholder`, `cachePolicy="memory-disk"`
- WebP format preferred

```tsx
import { Image } from 'expo-image';

<Image
  source="https://example.com/photo.jpg"
  placeholder={{ blurhash: '|rF?hV%2WCj[ayj[a|j[az' }}
  contentFit="cover"
  contentPosition="center"
  transition={300}
  cachePolicy="memory-disk"
  priority="high"
  recyclingKey={uri}
  allowDownscaling={true}
  autoplay={true}
  defaultSource={fallbackImage}
  accessibilityLabel="Photo"
  style={{ width: 200, height: 200 }}
/>;
```

## Bundle

- Import specific modules (`lodash/get` not `lodash`)
- `React.lazy` + `Suspense` for code splitting
- Target <1.5MB JS bundle

## Animations

- `react-native-reanimated` for ALL animations
- UI thread via worklets, never read shared values from JS in hot paths
- `useAnimatedStyle` for animated styles

## Startup

- Hermes engine
- Inline requires for heavy modules
- Minimize `useEffect` chains
- `InteractionManager.runAfterInteractions` for deferred work
