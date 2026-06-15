---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Gesture Handler

React Native Gesture Handler (v2/v3) -- native-driven gesture management. All gestures run on the native thread for 60fps interactions.

## Version detection

Check `package.json` version:
- **v2** -> Builder API (`Gesture.Pan()`, `Gesture.Simultaneous()`, **must wrap in `useMemo`**)
- **v3** -> Hook API (`usePanGesture()`, `useSimultaneousGestures()`, auto-memoized)

## v3 hook API

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

## Setup

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

## Critical rules

- **`GestureHandlerRootView` is mandatory** -- `GestureDetector` crashes without it as an ancestor
- **v2: `useMemo` every gesture** -- without it, gestures recreate on every render, losing state
- **Never call JS functions directly from gesture callbacks** -- use `scheduleOnRN` from `react-native-worklets` (`runOnJS` is removed in Reanimated 4)
- **Import `ScrollView`/`FlatList` from RNGH**, not `react-native`, when using gestures inside scroll containers
- **Never mix RN touch handlers with RNGH** in the same component tree
- **Don't add `'worklet'` to inline callbacks** -- auto-workletized by Babel plugin

## Gesture types

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

## Gesture composition

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

## Full example: draggable + scalable + rotatable

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

## Common patterns

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

## Swipeable components

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

## Testing gestures

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

## Gesture handler performance rules

- Gesture callbacks run on the **UI thread** by default -- don't access React state or call JS functions directly
- Use `scheduleOnRN(fn, args)` from `react-native-worklets` to call JS functions from gesture callbacks (`runOnJS` is removed in Reanimated 4)
- v2: wrap all gesture objects in `useMemo` -- v3 hooks handle this automatically
- Compose gestures with `Simultaneous`/`Exclusive`/`Race` instead of nesting `GestureDetector`s
- Use `activeOffsetX`/`activeOffsetY` on Pan to prevent accidental activation
- Pair with Reanimated `useSharedValue` + `useAnimatedStyle` for 60fps animations
- Prefer `Pressable` from gesture-handler over RN's `Pressable` for consistent native behavior
- Use `RectButton` (not `Pressable`) inside `ScrollView`/`FlatList` for native-feeling tap delay
