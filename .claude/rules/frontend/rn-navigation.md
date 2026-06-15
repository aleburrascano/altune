---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native navigation — Expo Router conventions

## Expo Router file conventions

Routes are defined by the file system under `apps/mobile/src/app/`:

```
apps/mobile/src/app/
├── _layout.tsx              # Root layout (providers, global UI)
├── (tabs)/
│   ├── _layout.tsx          # Tab navigator layout
│   ├── index.tsx            # Home tab (default)
│   ├── search.tsx           # Search tab
│   └── library.tsx          # Library tab
├── track/
│   └── [id].tsx             # Dynamic route: /track/:id
├── playlist/
│   ├── [id].tsx             # Dynamic route: /playlist/:id
│   └── create.tsx           # Static route: /playlist/create
├── settings/
│   ├── _layout.tsx          # Settings stack layout
│   └── index.tsx            # Settings screen
└── +not-found.tsx           # 404 fallback
```

- `_layout.tsx` — defines the navigator for that directory (Stack, Tabs, Drawer).
- `[param].tsx` — dynamic route segment, accessed via `useLocalSearchParams`.
- `(group)/` — layout group (parentheses), does not appear in the URL.
- `+not-found.tsx` — catch-all for unmatched routes.

## Typed routes and navigation

```tsx
import { useRouter, useLocalSearchParams, Link } from 'expo-router';

// Reading params
function TrackDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  // ...
}

// Imperative navigation
function SearchResult({ trackId }: { trackId: string }) {
  const router = useRouter();

  const handlePress = () => {
    router.push(`/track/${trackId}`);    // push onto stack
    // router.replace('/library');        // replace current screen
    // router.back();                     // go back
  };

  return <Pressable onPress={handlePress}>{ /* ... */ }</Pressable>;
}

// Declarative navigation
function TrackLink({ trackId, title }: { trackId: string; title: string }) {
  return (
    <Link href={`/track/${trackId}`} asChild>
      <Pressable>
        <Text>{title}</Text>
      </Pressable>
    </Link>
  );
}
```

## Deep linking

- Define the URL scheme in `app.json` under `expo.scheme`.
- Validate incoming URLs — never trust deep link params without sanitization.
- Test with `npx uri-scheme open <url> --ios` / `--android` during development.

## Modal patterns

- Use `presentation: 'modal'` in the route's screen options for modal presentation.
- Bottom sheets: use `@gorhom/bottom-sheet` for gesture-driven bottom sheets; integrate with Expo Router by presenting the sheet from a regular screen, not as a separate route (unless it needs its own URL).

```tsx
// In _layout.tsx
<Stack.Screen name="track/[id]" options={{ presentation: 'modal' }} />
```

## Best practices

- **Minimal navigation state**: do not store navigation state in Zustand or Context. Let Expo Router own it.
- **Prefetch on visibility**: use `queryClient.prefetchQuery` when a list item becomes visible to reduce perceived latency on navigation.
- **initialRouteName**: set in the layout's `<Tabs>` or `<Stack>` to control which screen shows first without a flash.
- **Avoid deep nesting**: keep the route tree shallow. Deeply nested stacks create confusing back-button behavior and complex state.
- **Loading states on navigation**: show skeleton/loading UI in the destination screen, not a blocking spinner on the source screen.
