---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Screens

React Native Screens (`react-native-screens`) -- native navigation container components. Powers Expo Router and React Navigation under the hood.

## Setup

```bash
npx expo install react-native-screens
```

**Note:** Expo Router includes this automatically.

## Enable native screens and freeze

```tsx
import { enableScreens, enableFreeze } from 'react-native-screens';

enableScreens();
enableFreeze(true);
```

## Presentation modes

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

## Form sheet configuration

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

## Animation types

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

## Header configuration

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

## SearchBar

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

## Transition progress hooks

```tsx
import { useTransitionProgress } from 'react-native-screens';

const { progress, closing, goingForward } = useTransitionProgress();

// Reanimated version
import { useReanimatedTransitionProgress } from 'react-native-screens/reanimated';
```

## Header height hooks

```tsx
import { useHeaderHeight } from 'react-native-screens/native-stack';
import { useAnimatedHeaderHeight } from 'react-native-screens/native-stack';

const headerHeight = useHeaderHeight();
const animatedHeight = useAnimatedHeaderHeight(); // tracks large title collapse
```

## Screen lifecycle events

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

## Screen freeze (performance)

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

## FullWindowOverlay

```tsx
import { FullWindowOverlay } from 'react-native-screens';

<FullWindowOverlay>
  <View style={StyleSheet.absoluteFill}>
    <Toast message="Hello" />
  </View>
</FullWindowOverlay>;
```

## Screens performance notes

- `enableFreeze(true)` -- most impactful single optimization for navigation performance
- Native transitions run on native thread -- zero JS thread overhead
- `activityState={0}` detaches screens entirely from native hierarchy (deep stacks)
- Use `contentInsetAdjustmentBehavior="automatic"` on ScrollView with large titles / search bar
- `headerTranslucent: true` required for scroll-under-header effect on iOS
