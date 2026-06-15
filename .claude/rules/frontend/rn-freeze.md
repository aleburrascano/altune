---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# react-freeze — prevent inactive screen re-renders

`react-freeze` suspends React rendering for off-screen components, preventing unnecessary re-renders when screens are not visible. Integrated with `react-native-screens` for navigation-aware freezing.

## Freeze component API

```tsx
import { Freeze } from 'react-freeze';

function MyScreen({ isActive }: { isActive: boolean }) {
  return (
    <Freeze freeze={!isActive}>
      <ExpensiveComponent />
    </Freeze>
  );
}
```

### Props

| Prop | Type | Default | Description |
|---|---|---|---|
| `freeze` | `boolean` | `false` | When `true`, suspends rendering of children |
| `placeholder` | `ReactNode` | `null` | Shown while children are frozen (optional) |

```tsx
<Freeze freeze={!isVisible} placeholder={<SkeletonLoader />}>
  <HeavyDashboard />
</Freeze>
```

## Integration with react-native-screens

The recommended approach is to enable freeze globally via `react-native-screens`, which automatically freezes screens that are not in the foreground.

```tsx
import { enableFreeze } from 'react-native-screens';

// Call once at app startup (before any navigation renders)
enableFreeze(true);
```

This automatically wraps every native screen in a `<Freeze>` component that activates when the screen is not in the active stack.

### Per-screen opt-in

```tsx
<Stack.Screen options={{ freezeOnBlur: true }} />
```

## State behavior while frozen

| Aspect | Behavior |
|---|---|
| React state (`useState`, `useReducer`) | Updates queued, applied on unfreeze |
| Zustand / Redux store updates | Subscriptions paused, latest state on unfreeze |
| TanStack Query refetches | Continue in background, UI updates on unfreeze |
| `useEffect` cleanup | Does NOT run (freeze is not unmount) |
| `useEffect` setup | Runs on unfreeze if deps changed |
| Native views (scroll position, inputs) | Preserved — native layer is untouched |
| Timers (`setTimeout`, `setInterval`) | Continue running (JS thread, not React) |
| Animations (Reanimated) | Continue on UI thread (not affected by freeze) |

## When to use

- **Navigation stacks**: enable globally with `enableFreeze(true)` — screens behind the active one stop re-rendering from store updates, query invalidations, and context changes.
- **Tab navigators**: inactive tabs stop consuming render cycles.
- **Conditional heavy content**: wrap expensive sub-trees that toggle visibility (e.g., dashboard panels, settings sections).

## Performance impact

- Largest single optimization for apps with deep navigation stacks or tab bars.
- Eliminates "background screen" re-renders from global state changes (theme toggle, auth state, real-time updates).
- Zero native overhead — freeze operates at the React reconciler level.

## Caveats

- `useEffect` cleanup does **not** run on freeze. If your effect relies on cleanup for correctness (e.g., unsubscribing from a WebSocket), the subscription stays active while frozen. This is usually fine (you want data fresh on unfreeze) but can be surprising.
- Frozen components do not respond to context changes until unfrozen. A theme change while a screen is frozen will apply when the user navigates back.
- `react-freeze` uses React Suspense internally. If your component tree already has Suspense boundaries, test that freeze interacts correctly.
