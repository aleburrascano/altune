---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native SVG

`react-native-svg` provides SVG support for React Native. Use for vector graphics, icons, charts, and custom shapes.

## Decision tree: when to use SVG

```
What are you rendering?
|
+-- Simple icon set (static, single color)?
|   -> Icon font (expo-vector-icons) or expo-image with SF Symbols
|
+-- Multi-color icon / logo / illustration?
|   -> react-native-svg (this file)
|
+-- Photo / raster image?
|   -> expo-image
|
+-- Complex animated illustration (Lottie/Bodymovin)?
|   -> lottie-react-native
|
+-- Charts, data visualization, custom drawing?
|   -> react-native-svg (simple) or @shopify/react-native-skia (complex)
|
+-- GPU-accelerated graphics, shaders, particles?
    -> @shopify/react-native-skia or react-native-wgpu
```

## Shape components

| Component | Key Props | Example |
|---|---|---|
| `<Rect>` | `x`, `y`, `width`, `height`, `rx`, `ry` | Rounded rectangle |
| `<Circle>` | `cx`, `cy`, `r` | Circle |
| `<Ellipse>` | `cx`, `cy`, `rx`, `ry` | Ellipse |
| `<Line>` | `x1`, `y1`, `x2`, `y2` | Straight line |
| `<Polyline>` | `points` | Connected line segments (open) |
| `<Polygon>` | `points` | Closed shape |
| `<Path>` | `d` | Arbitrary path (M, L, C, A commands) |

```tsx
import Svg, { Rect, Circle, Path, Line } from 'react-native-svg';

<Svg width={200} height={200} viewBox="0 0 200 200">
  <Rect x={10} y={10} width={80} height={80} rx={8} fill="#3B82F6" />
  <Circle cx={150} cy={50} r={40} fill="#EF4444" />
  <Line x1={10} y1={150} x2={190} y2={150} stroke="#6B7280" strokeWidth={2} />
  <Path d="M10 180 Q100 120 190 180" stroke="#10B981" strokeWidth={2} fill="none" />
</Svg>
```

## Common props

### Fill and stroke

```tsx
<Rect
  fill="#3B82F6"           // fill color
  fillOpacity={0.5}        // fill transparency
  stroke="#1E40AF"         // border color
  strokeWidth={2}          // border width
  strokeOpacity={1}        // border transparency
  strokeLinecap="round"    // 'butt' | 'round' | 'square'
  strokeLinejoin="round"   // 'miter' | 'round' | 'bevel'
  strokeDasharray="5,3"    // dash pattern
  strokeDashoffset={0}     // dash offset (animate for drawing effect)
/>
```

### Transform

```tsx
<Rect
  x={0} y={0} width={50} height={50}
  fill="blue"
  rotation={45}              // degrees
  origin="25, 25"            // transform origin
  scale={1.5}                // uniform scale
  translate="10, 20"         // translation
/>

// Or using transform string
<Rect transform="rotate(45, 25, 25) scale(1.5)" />
```

### Clipping and touch

```tsx
<Rect
  clipPath="url(#myClip)"     // clip to a defined path
  onPress={() => {}}           // touch handler
  onPressIn={() => {}}
  onPressOut={() => {}}
  onLongPress={() => {}}
/>
```

## Text

```tsx
import { Text as SvgText, TSpan } from 'react-native-svg';

<SvgText
  x={100}
  y={50}
  fill="#111827"
  fontSize={16}
  fontWeight="600"
  fontFamily="System"
  textAnchor="middle"        // 'start' | 'middle' | 'end'
  alignmentBaseline="middle" // vertical alignment
>
  Hello SVG
</SvgText>

// Multi-line / styled spans
<SvgText x={10} y={30}>
  <TSpan fill="black" fontSize={14}>Line one</TSpan>
  <TSpan x={10} dy={20} fill="gray" fontSize={12}>Line two</TSpan>
</SvgText>
```

## Gradients

### Linear gradient

```tsx
import { Defs, LinearGradient, Stop, Rect } from 'react-native-svg';

<Svg width={200} height={100}>
  <Defs>
    <LinearGradient id="grad" x1="0%" y1="0%" x2="100%" y2="0%">
      <Stop offset="0%" stopColor="#3B82F6" />
      <Stop offset="100%" stopColor="#8B5CF6" />
    </LinearGradient>
  </Defs>
  <Rect width={200} height={100} fill="url(#grad)" />
</Svg>
```

### Radial gradient

```tsx
import { Defs, RadialGradient, Stop, Circle } from 'react-native-svg';

<Defs>
  <RadialGradient id="radGrad" cx="50%" cy="50%" r="50%">
    <Stop offset="0%" stopColor="#FFFFFF" />
    <Stop offset="100%" stopColor="#3B82F6" />
  </RadialGradient>
</Defs>
<Circle cx={100} cy={100} r={80} fill="url(#radGrad)" />
```

## Clipping and masking

```tsx
import { Defs, ClipPath, Mask, Rect, Circle } from 'react-native-svg';

// ClipPath — hard edge
<Defs>
  <ClipPath id="clip">
    <Circle cx={100} cy={100} r={50} />
  </ClipPath>
</Defs>
<Rect width={200} height={200} fill="blue" clipPath="url(#clip)" />

// Mask — soft edge (alpha-based)
<Defs>
  <Mask id="mask">
    <Rect width={200} height={200} fill="white" />
    <Circle cx={100} cy={100} r={50} fill="black" />
  </Mask>
</Defs>
<Rect width={200} height={200} fill="red" mask="url(#mask)" />
```

## Patterns

```tsx
import { Defs, Pattern, Rect, Circle } from 'react-native-svg';

<Defs>
  <Pattern id="dots" x={0} y={0} width={20} height={20} patternUnits="userSpaceOnUse">
    <Circle cx={10} cy={10} r={3} fill="#6B7280" />
  </Pattern>
</Defs>
<Rect width={200} height={200} fill="url(#dots)" />
```

## Group, Defs, Use, Symbol

```tsx
import { G, Defs, Use, Symbol } from 'react-native-svg';

// Group — apply shared props to children
<G fill="blue" stroke="black" strokeWidth={1} opacity={0.8}>
  <Rect x={10} y={10} width={50} height={50} />
  <Circle cx={100} cy={35} r={25} />
</G>

// Symbol + Use — define once, reuse multiple times
<Defs>
  <Symbol id="icon" viewBox="0 0 24 24">
    <Path d="M12 2l3.09 6.26L22 9.27l-5 4.87..." fill="currentColor" />
  </Symbol>
</Defs>
<Use href="#icon" x={10} y={10} width={24} height={24} fill="gold" />
<Use href="#icon" x={50} y={10} width={24} height={24} fill="silver" />
```

## Animation with Reanimated

```tsx
import Animated, { useSharedValue, useAnimatedProps, withTiming } from 'react-native-reanimated';
import { Circle } from 'react-native-svg';

const AnimatedCircle = Animated.createAnimatedComponent(Circle);

function PulsingDot() {
  const radius = useSharedValue(20);

  const animatedProps = useAnimatedProps(() => ({
    r: radius.value,
  }));

  const startPulse = () => {
    radius.value = withTiming(40, { duration: 500 }, () => {
      radius.value = withTiming(20, { duration: 500 });
    });
  };

  return (
    <Svg width={100} height={100}>
      <AnimatedCircle cx={50} cy={50} fill="#3B82F6" animatedProps={animatedProps} />
    </Svg>
  );
}
```

### Stroke drawing animation

```tsx
const AnimatedPath = Animated.createAnimatedComponent(Path);

const progress = useSharedValue(0);
const animatedProps = useAnimatedProps(() => ({
  strokeDashoffset: pathLength * (1 - progress.value),
}));

<AnimatedPath
  d={pathData}
  stroke="#3B82F6"
  strokeWidth={2}
  fill="none"
  strokeDasharray={pathLength}
  animatedProps={animatedProps}
/>
```

## Rendering from XML / URI

```tsx
import { SvgXml, SvgUri } from 'react-native-svg';

// From XML string
const xml = `<svg viewBox="0 0 24 24"><path d="M12 2..." /></svg>`;
<SvgXml xml={xml} width={24} height={24} fill="#000" />

// From URI
<SvgUri uri="https://example.com/icon.svg" width={24} height={24} />
```

### SVG transformer (import .svg as components)

```bash
npx expo install react-native-svg-transformer
```

```tsx
// metro.config.js — configure transformer
// After setup:
import Logo from './assets/logo.svg';

<Logo width={120} height={40} fill="#3B82F6" />
```

## Performance rules

- **Minimize SVG node count** — each element is a native view. For >100 nodes, consider Skia Canvas.
- **Use `viewBox`** on the root `<Svg>` — allows scaling without recalculating coordinates.
- **Avoid re-rendering SVG trees** — wrap in `React.memo` if props don't change frequently.
- **Use `Animated.createAnimatedComponent`** for animated SVG props — runs on UI thread.
- **Prefer `<SvgXml>` over inline components** for static SVGs — avoids JSX overhead.
- **Don't use SVG for raster content** — use `expo-image` for photos and bitmaps.
- **Cache decoded SVGs** — if loading from network, cache the XML string or use `SvgUri` with a caching layer.
