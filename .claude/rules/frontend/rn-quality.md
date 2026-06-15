---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native quality: performance, testing, styling, animations, gestures

## Performance

### Rendering

- `React.memo` on list components, `useCallback` for memoized children, `useMemo` for expensive computations
- Never inline functions in JSX loops

### Lists

- FlashList for >20 items (preferred over FlatList)
- `keyExtractor`, `getItemLayout` for fixed heights
- `windowSize` / `maxToRenderPerBatch` / `initialNumToRender`
- `removeClippedSubviews` on Android

### Images

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

### Bundle

- Import specific modules (`lodash/get` not `lodash`)
- `React.lazy` + `Suspense` for code splitting
- Target <1.5MB JS bundle

### Animations

- `react-native-reanimated` for ALL animations
- UI thread via worklets, never read shared values from JS in hot paths
- `useAnimatedStyle` for animated styles

### Startup

- Hermes engine
- Inline requires for heavy modules
- Minimize `useEffect` chains
- `InteractionManager.runAfterInteractions` for deferred work

---

## Testing

### Stack

- Jest + RNTL for unit/component tests
- Detox for E2E
- Target 80% lines / 70% branches

### Principles

- Test behavior not implementation
- Query by role/text/label
- One assertion per test
- Mock at boundaries
- No snapshot tests as primary strategy

### Component test example

```tsx
it('disables submit when form is invalid', () => {
  render(<MyForm />);
  const submit = screen.getByRole('button', { name: /submit/i });
  expect(submit).toBeDisabled();
});
```

### Mocking

- `expo-secure-store` — mock the module
- `expo-router` — mock navigation
- MSW for API mocking

```tsx
// expo-constants mock
jest.mock('expo-constants', () => ({
  expoConfig: { extra: { apiUrl: 'http://test' } },
}));

// expo-router mock
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), back: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

// expo-image mock
jest.mock('expo-image', () => ({
  Image: 'Image',
}));
```

### File organization

- Tests adjacent to source (`__tests__/` dirs)
- Shared utils in `tests/helpers/`
- E2E in `e2e/`

### Detox with EAS Build

```json
{
  "build": {
    "test": {
      "ios": { "simulator": true },
      "android": { "gradleCommand": ":app:assembleDebug :app:assembleAndroidTest" },
      "env": { "DETOX_CONFIG": ".detoxrc.js" }
    }
  }
}
```

- Use `expo-dev-client` for native module testing

---

## Styling

### StyleSheet.create patterns

Always use `StyleSheet.create` for component styles. Never use inline styles.

```tsx
import { StyleSheet, View, Text } from 'react-native';

export function Card({ title, children }: CardProps) {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>{title}</Text>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    backgroundColor: '#FFFFFF',
    borderRadius: 16,
    padding: 16,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.08,
    shadowRadius: 8,
    elevation: 3,
  },
  title: {
    fontSize: 18,
    fontWeight: '600',
    color: '#111827',
  },
});
```

### Theme tokens

```tsx
// theme/tokens.ts
export const colors = {
  light: {
    background: '#FFFFFF',
    surface: '#F9FAFB',
    text: '#111827',
    textSecondary: '#6B7280',
    primary: '#3B82F6',
    error: '#EF4444',
    border: '#E5E7EB',
  },
  dark: {
    background: '#111827',
    surface: '#1F2937',
    text: '#F9FAFB',
    textSecondary: '#9CA3AF',
    primary: '#60A5FA',
    error: '#F87171',
    border: '#374151',
  },
} as const;

export const spacing = {
  xs: 4,
  sm: 8,
  md: 16,
  lg: 24,
  xl: 32,
} as const;

export const typography = {
  h1: { fontSize: 32, fontWeight: '700' as const },
  h2: { fontSize: 24, fontWeight: '600' as const },
  body: { fontSize: 16, fontWeight: '400' as const },
  caption: { fontSize: 12, fontWeight: '400' as const },
} as const;
```

### Dark mode with useColorScheme

```tsx
import { useColorScheme, StyleSheet, View, Text } from 'react-native';
import { colors, spacing } from '@/theme/tokens';

export function ThemedCard({ title, children }: CardProps) {
  const colorScheme = useColorScheme();
  const theme = colors[colorScheme ?? 'light'];

  return (
    <View style={[styles.container, { backgroundColor: theme.surface }]}>
      <Text style={[styles.title, { color: theme.text }]}>{title}</Text>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    borderRadius: 16,
    padding: spacing.md,
  },
  title: {
    fontSize: 18,
    fontWeight: '600',
  },
});
```

### useThemedStyles hook

```tsx
// hooks/useThemedStyles.ts
import { useMemo } from 'react';
import { StyleSheet, useColorScheme } from 'react-native';
import { colors } from '@/theme/tokens';

type StyleFactory<T extends StyleSheet.NamedStyles<T>> = (theme: typeof colors.light) => T;

export function useThemedStyles<T extends StyleSheet.NamedStyles<T>>(factory: StyleFactory<T>): T {
  const colorScheme = useColorScheme();
  const theme = colors[colorScheme ?? 'light'];
  return useMemo(() => StyleSheet.create(factory(theme)), [theme]);
}

// Usage
function ProfileScreen() {
  const styles = useThemedStyles((theme) => ({
    container: {
      flex: 1,
      backgroundColor: theme.background,
    },
    heading: {
      color: theme.text,
      fontSize: 24,
      fontWeight: '700',
    },
  }));

  return (
    <View style={styles.container}>
      <Text style={styles.heading}>Profile</Text>
    </View>
  );
}
```

### New CSS properties (New Architecture, RN 0.77+)

- `display: 'contents'` -- wrapper elements that don't affect layout
- `boxSizing: 'border-box' | 'content-box'` -- box model control
- `mixBlendMode` -- blend modes (multiply, screen, overlay, etc.)
- `outlineWidth`, `outlineStyle`, `outlineSpread`, `outlineColor` -- outlines without layout impact
- `box-shadow` and `filter` now require CSS units (e.g., `'1px'` not `1`) since RN 0.79

### Apple HIG styling patterns

- Use `borderCurve: 'continuous'` for Apple-style smooth rounded corners (not default circular)
- Prefer `gap` (flex gap) over margins/padding for spacing between siblings
- Use `fontVariant: ['tabular-nums']` for numeric counters and timers (uniform digit width)
- Add `selectable` prop to `<Text>` displaying critical data (IDs, codes, URLs)
- Use CSS `boxShadow` for shadows (supports `inset` keyword)
- Use `experimental_backgroundImage` for CSS gradients (New Architecture only)
- Use `useWindowDimensions()` over `Dimensions.get()` -- reactive to changes
- Prefer `process.env.EXPO_OS` over `Platform.OS` for platform checks

```tsx
const styles = StyleSheet.create({
  card: {
    borderRadius: 16,
    borderCurve: 'continuous',
  },
  counter: {
    fontVariant: ['tabular-nums'],
  },
  shadow: {
    boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
  },
});
```

### Styling rules

- Always use `StyleSheet.create` -- never inline style objects
- Use theme tokens for colors, spacing, and typography -- no magic numbers
- Support dark mode via `useColorScheme` and themed token sets
- Keep styles at the bottom of the file, colocated with the component
- Prefer `process.env.EXPO_OS` over `Platform.select` for platform-specific logic
- Compose styles with array syntax: `style={[styles.base, styles.variant]}`
- Conditional styles: `style={[styles.base, isActive && styles.active]}`
- Never use `Dimensions.get()` -- use `useWindowDimensions()` hook
- Never use intrinsic elements (`<div>`, `<img>`, `<span>`) outside WebViews
- `Appearance.setColorScheme(null)` no longer works -- use `'unspecified'` instead (RN 0.82+)

---

## Reanimated

React Native Reanimated (v4) -- declarative, performant animation library. Animations run on the UI thread via worklets. **Reanimated v4 requires New Architecture** (mandatory since RN 0.82).

### Core concepts

#### Shared values

```tsx
import { useSharedValue } from 'react-native-reanimated';

const offset = useSharedValue(0);
offset.value = 100;
```

#### Animated styles

```tsx
import Animated, { useSharedValue, useAnimatedStyle, withSpring } from 'react-native-reanimated';

function Box() {
  const offset = useSharedValue(0);

  const animatedStyle = useAnimatedStyle(() => ({
    transform: [{ translateX: offset.value }],
  }));

  return (
    <>
      <Animated.View style={[styles.box, animatedStyle]} />
      <Button
        onPress={() => {
          offset.value = withSpring(200);
        }}
        title="Move"
      />
    </>
  );
}
```

#### Derived values

```tsx
import { useDerivedValue } from 'react-native-reanimated';

const scale = useSharedValue(1);
const opacity = useDerivedValue(() => {
  return scale.value > 1.5 ? 1 : 0.5;
});
```

#### Animated props (non-style props)

```tsx
import Animated, { useAnimatedProps } from 'react-native-reanimated';
import Svg, { Circle } from 'react-native-svg';

const AnimatedCircle = Animated.createAnimatedComponent(Circle);

const r = useSharedValue(50);
const animatedProps = useAnimatedProps(() => ({ r: r.value }));

<AnimatedCircle animatedProps={animatedProps} cx={100} cy={100} fill="blue" />;
```

### Animation functions

```tsx
import { withTiming, withSpring, withDecay, Easing } from 'react-native-reanimated';

// withTiming (duration-based)
offset.value = withTiming(200);
offset.value = withTiming(200, {
  duration: 500,
  easing: Easing.out(Easing.cubic),
});
offset.value = withTiming(200, { duration: 300 }, (finished) => {
  'worklet';
  if (finished) scheduleOnRN(onComplete);
});

// withSpring (physics-based)
offset.value = withSpring(200);
offset.value = withSpring(200, {
  damping: 20,
  stiffness: 90,
  mass: 1,
  overshootClamping: false,
  restDisplacementThreshold: 0.01,
  restSpeedThreshold: 2,
});

// withDecay (momentum-based, e.g. after fling gesture)
offset.value = withDecay({
  velocity: velocityFromGesture,
  deceleration: 0.998,
  clamp: [0, 500],
  rubberBandEffect: true,
  rubberBandFactor: 0.6,
});
```

### Modifiers (compose animations)

```tsx
import { withRepeat, withSequence, withDelay, cancelAnimation } from 'react-native-reanimated';

offset.value = withRepeat(withTiming(100), 5, true);  // 5 times, reverse
offset.value = withRepeat(withTiming(100), -1, true);  // infinite

offset.value = withSequence(
  withTiming(-50, { duration: 100 }),
  withRepeat(withTiming(50, { duration: 200 }), 5, true),
  withTiming(0, { duration: 100 }),
);

offset.value = withDelay(500, withSpring(200));
cancelAnimation(offset);
```

### withClamp (limits range, prevents spring overshoot)

```tsx
offset.value = withClamp({ min: 0, max: 300 }, withSpring(200));
```

### Easing functions

```tsx
import { Easing } from 'react-native-reanimated';

Easing.linear;
Easing.ease;
Easing.quad;
Easing.cubic;
Easing.poly(n);
Easing.sin;
Easing.circle;
Easing.exp;
Easing.elastic(n);
Easing.back(s);
Easing.bounce;

// Modifiers
Easing.in(Easing.cubic);
Easing.out(Easing.cubic);
Easing.inOut(Easing.cubic);
Easing.bezier(x1, y1, x2, y2);
```

### Layout animations

```tsx
import Animated, {
  FadeIn, FadeInDown, FadeOut,
  SlideInLeft, SlideOutRight,
  ZoomIn, BounceIn,
  LinearTransition, CurvedTransition, EntryExitTransition,
} from 'react-native-reanimated';

// Entering
<Animated.View entering={FadeIn} />
<Animated.View entering={FadeInDown.duration(500).delay(200).springify()} />

// Exiting
<Animated.View exiting={FadeOut.duration(300)} />

// Layout transitions (when position/size changes)
<Animated.View layout={LinearTransition.springify()} />
<Animated.View layout={CurvedTransition
  .easingX(Easing.in(Easing.exp))
  .easingY(Easing.out(Easing.quad))
  .duration(500)
} />
```

### Scroll handling

```tsx
import Animated, { useAnimatedScrollHandler, useSharedValue } from 'react-native-reanimated';

function ScrollExample() {
  const scrollY = useSharedValue(0);

  const scrollHandler = useAnimatedScrollHandler({
    onScroll: (event) => {
      scrollY.value = event.contentOffset.y;
    },
    onBeginDrag: (event) => {},
    onEndDrag: (event) => {},
    onMomentumBegin: (event) => {},
    onMomentumEnd: (event) => {},
  });

  return (
    <Animated.ScrollView onScroll={scrollHandler} scrollEventThrottle={16}>
      {/* content */}
    </Animated.ScrollView>
  );
}
```

### Thread communication (Reanimated v4)

**`runOnJS` is removed in Reanimated 4.** Use `scheduleOnRN` from `react-native-worklets` instead.

```tsx
import { scheduleOnRN } from 'react-native-worklets';
import { scheduleOnUI } from 'react-native-worklets';

// Call JS function from UI thread (worklet -> JS)
const showAlert = (message: string) => {
  Alert.alert(message);
};

const handler = useAnimatedScrollHandler({
  onScroll: (event) => {
    if (event.contentOffset.y > 500) {
      scheduleOnRN(showAlert, 'Scrolled past 500!');
    }
  },
});

// Call worklet from JS thread (JS -> UI)
scheduleOnUI(() => {
  'worklet';
  offset.value = withSpring(100);
});
```

**Key difference:** `scheduleOnRN(fn, arg1, arg2)` passes args directly (not curried like the old `runOnJS(fn)(args)`).

### CSS Transitions (Reanimated 4)

Simplest animation approach -- animate style changes automatically when state changes:

```tsx
const [expanded, setExpanded] = useState(false);

<Animated.View
  style={{
    height: expanded ? 200 : 100,
    opacity: expanded ? 1 : 0.5,
    transitionProperty: 'height, opacity',
    transitionDuration: '300ms',
    transitionTimingFunction: 'ease-in-out',
  }}
/>
```

### CSS Animations (Reanimated 4)

Keyframe-based animations using CSS syntax:

```tsx
const pulse = {
  '0%': { transform: [{ scale: 1 }], opacity: 1 },
  '50%': { transform: [{ scale: 1.1 }], opacity: 0.7 },
  '100%': { transform: [{ scale: 1 }], opacity: 1 },
};

<Animated.View
  style={{
    animationName: pulse,
    animationDuration: '1s',
    animationIterationCount: 'infinite',
    animationTimingFunction: 'ease-in-out',
  }}
/>
```

### Interpolation

```tsx
import { interpolate, interpolateColor, Extrapolation } from 'react-native-reanimated';

const animatedStyle = useAnimatedStyle(() => ({
  opacity: interpolate(scrollY.value, [0, 100], [1, 0], Extrapolation.CLAMP),
  transform: [
    {
      scale: interpolate(scrollY.value, [0, 100], [1, 0.8], Extrapolation.CLAMP),
    },
  ],
  backgroundColor: interpolateColor(progress.value, [0, 1], ['#FF0000', '#0000FF']),
}));
```

### Animation decision tree

```
What are you animating?
|
+-- Simple state-driven style change (opacity, color, size)?
|   -> CSS Transitions -- simplest, automatic
|
+-- Repeating/looping animation (pulse, spin, shimmer)?
|   -> CSS Animations with keyframes
|
+-- Gesture-driven or interactive animation?
|   -> Shared Values + useAnimatedStyle
|
+-- Complex 2D graphics (charts, drawing, particles)?
|   -> Skia Canvas (@shopify/react-native-skia)
|
+-- GPU compute (physics sim, boids, fluid, noise)?
    -> WebGPU (react-native-wgpu + TypeGPU)
```

### Animated components

```tsx
import Animated from 'react-native-reanimated';

<Animated.View />
<Animated.Text />
<Animated.Image />
<Animated.ScrollView />
<Animated.FlatList />

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);
```

### Accessibility

```tsx
import { useReducedMotion } from 'react-native-reanimated';

const reduceMotion = useReducedMotion();
const duration = reduceMotion ? 0 : 500;
offset.value = withTiming(200, { duration });

<Animated.View entering={FadeIn.reduceMotion(ReduceMotion.System)} />;
```

### useSharedValue gotchas

- **Never destructure:** `const { value } = sv` breaks reactivity. Always use `sv.value`
- **Use `.modify()` for arrays/objects:** `arr.modify((v) => { v.push(item); })` -- avoids copying
- **React Compiler:** Use `.get()` / `.set()` instead of `.value` for compatibility
- **Never read/modify during render:** Only inside `useAnimatedStyle`, `useDerivedValue`, or worklet callbacks
- **Don't add `'worklet'` to callbacks passed to Reanimated APIs** -- auto-workletized by Babel plugin

### Reanimated performance rules

- All animation code runs on the UI thread -- never access React state or JS-only APIs inside `useAnimatedStyle` or worklets
- Use `scheduleOnRN()` from `react-native-worklets` to call JS functions from worklets (`runOnJS` is removed)
- Use `useDerivedValue` for computed values (reactive, runs on UI thread)
- Prefer `withSpring` over `withTiming` for natural-feeling animations
- Use `cancelAnimation(sharedValue)` before starting a new animation on the same value
- Always use `Animated.View`/`Animated.Text` etc. -- regular RN components don't animate
- Use `useReducedMotion()` to respect accessibility preferences
- Prefer non-layout properties (`transform`, `opacity`) over layout properties (`top`, `left`, `width`, `height`) -- layout forces extra passes
- **Reanimated v4:** `react-native-worklets` is a separate package -- installed automatically

---

## Gesture Handler

React Native Gesture Handler (v2/v3) -- native-driven gesture management. All gestures run on the native thread for 60fps interactions.

### Version detection

Check `package.json` version:
- **v2** -> Builder API (`Gesture.Pan()`, `Gesture.Simultaneous()`, **must wrap in `useMemo`**)
- **v3** -> Hook API (`usePanGesture()`, `useSimultaneousGestures()`, auto-memoized)

### v3 hook API

```tsx
import { usePanGesture, useTapGesture, useSimultaneousGestures } from 'react-native-gesture-handler';
import { scheduleOnRN } from 'react-native-worklets';

const pan = usePanGesture({
  onBegin: () => { startX.value = offsetX.value; },
  onUpdate: (e) => { offsetX.value = startX.value + e.translationX; },
  onDeactivate: (e) => {
    offsetX.value = withSpring(0);
    scheduleOnRN(onDragEnd, offsetX.value);
  },
});
```

| v2 Builder API | v3 Hook API |
|---|---|
| `Gesture.Pan().onUpdate(...)` | `usePanGesture({ onUpdate: ... })` |
| `Gesture.Simultaneous(a, b)` | `useSimultaneousGestures(a, b)` |
| `Gesture.Race(a, b)` | `useCompetingGestures(a, b)` |
| `Gesture.Exclusive(a, b)` | `useExclusiveGestures(a, b)` |
| `.onStart(...)` | `onActivate: ...` |
| `.onEnd(...)` | `onDeactivate: ...` |
| `.onChange(...)` | merged into `onUpdate` (use `changeX`, `changeY`) |
| Wrap in `useMemo` (mandatory) | Auto-memoized by hooks |

### Setup

```bash
npx expo install react-native-gesture-handler
```

Wrap your app root:

```tsx
import { GestureHandlerRootView } from 'react-native-gesture-handler';

export default function App() {
  return <GestureHandlerRootView style={{ flex: 1 }}>{/* app content */}</GestureHandlerRootView>;
}
```

**Note:** Expo Router wraps in `GestureHandlerRootView` automatically.

### Critical rules

- **`GestureHandlerRootView` is mandatory** -- `GestureDetector` crashes without it as an ancestor
- **v2: `useMemo` every gesture** -- without it, gestures recreate on every render, losing state
- **Never call JS functions directly from gesture callbacks** -- use `scheduleOnRN` from `react-native-worklets` (`runOnJS` is removed in Reanimated 4)
- **Import `ScrollView`/`FlatList` from RNGH**, not `react-native`, when using gestures inside scroll containers
- **Never mix RN touch handlers with RNGH** in the same component tree
- **Don't add `'worklet'` to inline callbacks** -- auto-workletized by Babel plugin

### Gesture types

```tsx
// Tap
const tap = Gesture.Tap()
  .numberOfTaps(1)
  .maxDuration(250)
  .maxDelay(250)
  .maxDistance(10)
  .onStart((e) => {});

// Double tap
const doubleTap = Gesture.Tap()
  .numberOfTaps(2)
  .onStart(() => {});

// Pan (drag)
const pan = Gesture.Pan()
  .minDistance(10)
  .activeOffsetX([-20, 20])
  .activeOffsetY([-20, 20])
  .minPointers(1)
  .maxPointers(1)
  .onUpdate((e) => {
    // e.translationX, e.translationY, e.velocityX, e.velocityY
    offset.value = startPos.value + e.translationX;
  })
  .onEnd((e) => {
    offset.value = withDecay({ velocity: e.velocityX });
  });

// Pinch (scale)
const pinch = Gesture.Pinch()
  .onUpdate((e) => {
    // e.scale, e.focalX, e.focalY, e.velocity
    scale.value = savedScale.value * e.scale;
  })
  .onEnd(() => {
    savedScale.value = scale.value;
  });

// Rotation
const rotation = Gesture.Rotation()
  .onUpdate((e) => {
    // e.rotation (radians), e.anchorX, e.anchorY, e.velocity
    rotate.value = savedRotation.value + e.rotation;
  })
  .onEnd(() => {
    savedRotation.value = rotate.value;
  });

// Long Press
const longPress = Gesture.LongPress()
  .minDuration(500)
  .maxDistance(10)
  .onStart(() => {})
  .onEnd((e, success) => {});

// Fling (swipe)
const fling = Gesture.Fling()
  .direction(Directions.RIGHT)
  .numberOfPointers(1)
  .onStart(() => {});
```

### Gesture composition

```tsx
// Simultaneous (all at once)
const composed = Gesture.Simultaneous(pan, pinch, rotate);

// Exclusive (first match wins)
const composed = Gesture.Exclusive(doubleTap, singleTap);

// Race (first to activate wins)
const composed = Gesture.Race(tap, pan, pinch);

// Cross-gesture dependencies
singleTap.requireExternalGestureToFail(doubleTap);
pan.blocksExternalGesture(externalPan);
swipeable.simultaneousWithExternalGesture(scrollGesture);
```

### Full example: draggable + scalable + rotatable

```tsx
import { GestureDetector, Gesture } from 'react-native-gesture-handler';
import Animated, { useSharedValue, useAnimatedStyle, withSpring } from 'react-native-reanimated';

function TransformableBox() {
  const offsetX = useSharedValue(0);
  const offsetY = useSharedValue(0);
  const startX = useSharedValue(0);
  const startY = useSharedValue(0);
  const scale = useSharedValue(1);
  const savedScale = useSharedValue(1);
  const rotation = useSharedValue(0);
  const savedRotation = useSharedValue(0);

  const pan = Gesture.Pan()
    .onBegin(() => {
      startX.value = offsetX.value;
      startY.value = offsetY.value;
    })
    .onUpdate((e) => {
      offsetX.value = startX.value + e.translationX;
      offsetY.value = startY.value + e.translationY;
    });

  const pinch = Gesture.Pinch()
    .onUpdate((e) => {
      scale.value = savedScale.value * e.scale;
    })
    .onEnd(() => {
      savedScale.value = scale.value;
    });

  const rotate = Gesture.Rotation()
    .onUpdate((e) => {
      rotation.value = savedRotation.value + e.rotation;
    })
    .onEnd(() => {
      savedRotation.value = rotation.value;
    });

  const composed = Gesture.Simultaneous(pan, pinch, rotate);

  const animatedStyle = useAnimatedStyle(() => ({
    transform: [
      { translateX: offsetX.value },
      { translateY: offsetY.value },
      { scale: scale.value },
      { rotateZ: `${(rotation.value * 180) / Math.PI}deg` },
    ],
  }));

  return (
    <GestureDetector gesture={composed}>
      <Animated.View style={[styles.box, animatedStyle]} />
    </GestureDetector>
  );
}
```

### Common patterns

```tsx
// Swipe to dismiss
const pan = Gesture.Pan()
  .onUpdate((e) => {
    translateY.value = Math.max(0, e.translationY);
    opacity.value = 1 - translateY.value / 300;
  })
  .onEnd((e) => {
    if (translateY.value > 150 || e.velocityY > 500) {
      translateY.value = withTiming(500);
      scheduleOnRN(onDismiss);
    } else {
      translateY.value = withSpring(0);
      opacity.value = withSpring(1);
    }
  });

// Bottom sheet drag
const pan = Gesture.Pan()
  .onUpdate((e) => {
    sheetY.value = Math.max(0, startY.value + e.translationY);
  })
  .onEnd((e) => {
    const snapTo = snapPoints.reduce((prev, curr) =>
      Math.abs(curr - sheetY.value) < Math.abs(prev - sheetY.value) ? curr : prev,
    );
    sheetY.value = withSpring(snapTo, { damping: 20 });
  });
```

### Swipeable components

```tsx
import ReanimatedSwipeable from 'react-native-gesture-handler/ReanimatedSwipeable';
import Reanimated, { SharedValue, useAnimatedStyle } from 'react-native-reanimated';

function RightAction(prog: SharedValue<number>, drag: SharedValue<number>) {
  const style = useAnimatedStyle(() => ({
    transform: [{ translateX: drag.value + 50 }],
  }));

  return (
    <Reanimated.View style={style}>
      <Text style={{ width: 50, height: 50, backgroundColor: 'red' }}>Delete</Text>
    </Reanimated.View>
  );
}

<ReanimatedSwipeable
  friction={2}
  rightThreshold={40}
  enableTrackpadTwoFingerGesture
  renderRightActions={RightAction}
  simultaneousWithExternalGesture={scrollGesture}
>
  <Text>Swipe me</Text>
</ReanimatedSwipeable>;
```

### Testing gestures

```tsx
import { fireGestureHandler, getByGestureTestId } from 'react-native-gesture-handler/jest-utils';
import { State } from 'react-native-gesture-handler';

// Add testID to gesture
const tap = useTapGesture({ testID: 'my-tap', disableReanimated: true, onDeactivate: handler });

// In test
const gesture = getByGestureTestId('my-tap');
fireGestureHandler(gesture, [
  { state: State.BEGAN },
  { state: State.ACTIVE },
  { state: State.END },
]);
```

Set `disableReanimated: true` in tests to run callbacks synchronously on JS thread.

### Gesture handler performance rules

- Gesture callbacks run on the **UI thread** by default -- don't access React state or call JS functions directly
- Use `scheduleOnRN(fn, args)` from `react-native-worklets` to call JS functions from gesture callbacks (`runOnJS` is removed in Reanimated 4)
- v2: wrap all gesture objects in `useMemo` -- v3 hooks handle this automatically
- Compose gestures with `Simultaneous`/`Exclusive`/`Race` instead of nesting `GestureDetector`s
- Use `activeOffsetX`/`activeOffsetY` on Pan to prevent accidental activation
- Pair with Reanimated `useSharedValue` + `useAnimatedStyle` for 60fps animations
- Prefer `Pressable` from gesture-handler over RN's `Pressable` for consistent native behavior
- Use `RectButton` (not `Pressable`) inside `ScrollView`/`FlatList` for native-feeling tap delay

---

## Screens

React Native Screens (`react-native-screens`) -- native navigation container components. Powers Expo Router and React Navigation under the hood.

### Setup

```bash
npx expo install react-native-screens
```

**Note:** Expo Router includes this automatically.

### Enable native screens and freeze

```tsx
import { enableScreens, enableFreeze } from 'react-native-screens';

enableScreens();
enableFreeze(true);
```

### Presentation modes

```tsx
<Stack.Screen
  name="detail"
  options={{
    presentation: 'push',              // standard push (default)
    presentation: 'modal',             // iOS: card modal, Android: slide up
    presentation: 'transparentModal',  // transparent overlay
    presentation: 'formSheet',         // iOS form sheet with detents
    presentation: 'fullScreenModal',   // full screen modal
    presentation: 'containedModal',    // modal within current context
  }}
/>
```

### Form sheet configuration

```tsx
<Stack.Screen
  name="edit"
  options={{
    presentation: 'formSheet',
    sheetAllowedDetents: [0.25, 0.5, 0.75, 1.0],
    sheetLargestUndimmedDetent: 0,
    sheetGrabberVisible: true,
    sheetCornerRadius: 20,
    sheetExpandsWhenScrolledToEdge: true,
    gestureEnabled: true,
    preventNativeDismiss: true,
  }}
/>
```

### Animation types

```tsx
<Stack.Screen
  options={{
    animation: 'default',           // platform default
    animation: 'fade',              // cross-fade
    animation: 'fade_from_bottom',  // fade + slide from bottom
    animation: 'flip',              // flip transition
    animation: 'slide_from_right',  // standard iOS push
    animation: 'slide_from_left',   // reverse push
    animation: 'slide_from_bottom', // slide up
    animation: 'ios_from_right',    // iOS-style on Android
    animation: 'none',              // instant
    transitionDuration: 350,        // ms (iOS only)
  }}
/>
```

### Header configuration

```tsx
<Stack.Screen
  options={{
    headerShown: true,
    headerTranslucent: true,
    headerBlurEffect: 'regular',
    title: 'Settings',
    headerTitleStyle: { fontSize: 18, fontWeight: '600' },
    headerLargeTitle: true,
    headerLargeTitleStyle: { fontSize: 34 },
    headerStyle: { backgroundColor: '#fff' },
    headerTintColor: '#007AFF',
    headerBackTitle: 'Back',
    headerBackButtonDisplayMode: 'minimal',
    statusBarStyle: 'dark',
    statusBarAnimation: 'fade',
    screenOrientation: 'portrait',
  }}
/>
```

### SearchBar

```tsx
<Stack.Screen
  options={{
    headerSearchBarOptions: {
      placeholder: 'Search...',
      autoCapitalize: 'none',
      hideWhenScrolling: true,
      obscureBackground: true,
      onChangeText: (e) => setQuery(e.nativeEvent.text),
      onSearchButtonPress: (e) => handleSearch(e.nativeEvent.text),
      onCancelButtonPress: () => setQuery(''),
    },
  }}
/>;

searchBarRef.current?.focus();
searchBarRef.current?.blur();
searchBarRef.current?.clearText();
searchBarRef.current?.setText('query');
searchBarRef.current?.cancelSearch();
```

### Transition progress hooks

```tsx
import { useTransitionProgress } from 'react-native-screens';

const { progress, closing, goingForward } = useTransitionProgress();

// Reanimated version
import { useReanimatedTransitionProgress } from 'react-native-screens/reanimated';
```

### Header height hooks

```tsx
import { useHeaderHeight } from 'react-native-screens/native-stack';
import { useAnimatedHeaderHeight } from 'react-native-screens/native-stack';

const headerHeight = useHeaderHeight();
const animatedHeight = useAnimatedHeaderHeight(); // tracks large title collapse
```

### Screen lifecycle events

```tsx
<Stack.Screen
  listeners={{
    appear: () => {},
    disappear: () => {},
    dismiss: () => {},
    willAppear: () => {},
    willDisappear: () => {},
  }}
/>
```

### Screen freeze (performance)

```tsx
// Global (recommended)
enableFreeze(true);

// Per-screen
<Stack.Screen options={{ freezeOnBlur: true }} />;
```

When frozen:
- React tree rendering paused (no re-renders from store updates)
- Native views preserved (scroll position, inputs, images)
- `useEffect` cleanup does NOT run (not an unmount)
- On unfreeze, renders once with current state

### FullWindowOverlay

```tsx
import { FullWindowOverlay } from 'react-native-screens';

<FullWindowOverlay>
  <View style={StyleSheet.absoluteFill}>
    <Toast message="Hello" />
  </View>
</FullWindowOverlay>;
```

### Screens performance notes

- `enableFreeze(true)` -- most impactful single optimization for navigation performance
- Native transitions run on native thread -- zero JS thread overhead
- `activityState={0}` detaches screens entirely from native hierarchy (deep stacks)
- Use `contentInsetAdjustmentBehavior="automatic"` on ScrollView with large titles / search bar
- `headerTranslucent: true` required for scroll-under-header effect on iOS
