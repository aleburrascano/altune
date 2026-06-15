---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Reanimated v4

React Native Reanimated (v4) -- declarative, performant animation library. Animations run on the UI thread via worklets. **Reanimated v4 requires New Architecture** (mandatory since RN 0.82).

## Core concepts

### Shared values

```tsx
import { useSharedValue } from 'react-native-reanimated';

const offset = useSharedValue(0);
offset.value = 100;
```

### Animated styles

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

### Derived values

```tsx
import { useDerivedValue } from 'react-native-reanimated';

const scale = useSharedValue(1);
const opacity = useDerivedValue(() => {
  return scale.value > 1.5 ? 1 : 0.5;
});
```

### Animated props (non-style props)

```tsx
import Animated, { useAnimatedProps } from 'react-native-reanimated';
import Svg, { Circle } from 'react-native-svg';

const AnimatedCircle = Animated.createAnimatedComponent(Circle);

const r = useSharedValue(50);
const animatedProps = useAnimatedProps(() => ({ r: r.value }));

<AnimatedCircle animatedProps={animatedProps} cx={100} cy={100} fill="blue" />;
```

## Animation functions

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

## Modifiers (compose animations)

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

## withClamp (limits range, prevents spring overshoot)

```tsx
offset.value = withClamp({ min: 0, max: 300 }, withSpring(200));
```

## Easing functions

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

## Layout animations

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

## Scroll handling

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

## Thread communication (Reanimated v4)

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

## CSS Transitions (Reanimated 4)

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

## CSS Animations (Reanimated 4)

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

## Interpolation

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

## Animation decision tree

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

## Animated components

```tsx
import Animated from 'react-native-reanimated';

<Animated.View />
<Animated.Text />
<Animated.Image />
<Animated.ScrollView />
<Animated.FlatList />

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);
```

## Accessibility

```tsx
import { useReducedMotion } from 'react-native-reanimated';

const reduceMotion = useReducedMotion();
const duration = reduceMotion ? 0 : 500;
offset.value = withTiming(200, { duration });

<Animated.View entering={FadeIn.reduceMotion(ReduceMotion.System)} />;
```

## useSharedValue gotchas

- **Never destructure:** `const { value } = sv` breaks reactivity. Always use `sv.value`
- **Use `.modify()` for arrays/objects:** `arr.modify((v) => { v.push(item); })` -- avoids copying
- **React Compiler:** Use `.get()` / `.set()` instead of `.value` for compatibility
- **Never read/modify during render:** Only inside `useAnimatedStyle`, `useDerivedValue`, or worklet callbacks
- **Don't add `'worklet'` to callbacks passed to Reanimated APIs** -- auto-workletized by Babel plugin

## Reanimated performance rules

- All animation code runs on the UI thread -- never access React state or JS-only APIs inside `useAnimatedStyle` or worklets
- Use `scheduleOnRN()` from `react-native-worklets` to call JS functions from worklets (`runOnJS` is removed)
- Use `useDerivedValue` for computed values (reactive, runs on UI thread)
- Prefer `withSpring` over `withTiming` for natural-feeling animations
- Use `cancelAnimation(sharedValue)` before starting a new animation on the same value
- Always use `Animated.View`/`Animated.Text` etc. -- regular RN components don't animate
- Use `useReducedMotion()` to respect accessibility preferences
- Prefer non-layout properties (`transform`, `opacity`) over layout properties (`top`, `left`, `width`, `height`) -- layout forces extra passes
- **Reanimated v4:** `react-native-worklets` is a separate package -- installed automatically
