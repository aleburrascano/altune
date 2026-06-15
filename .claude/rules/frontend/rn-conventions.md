---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native conventions: coding style, patterns, state, navigation

## Coding style

### TypeScript

- Strict mode always (`strict: true`). No `any` — use `unknown` + type guards.
- Prefer `interface` for object shapes, `type` for unions and intersections.
- Use `as const` over enums for literal types.
- Use `import type` for type-only imports.
- Discriminated unions for state machines (loading / loaded / error).

### Components

- Functional components only. No class components.
- Named exports only — no default exports.
- One component per file.
- Props interface named `[Component]Props` (e.g., `TrackCardProps`).
- Destructure props in the function signature.

```tsx
interface TrackCardProps {
  track: Track;
  onPress: (id: TrackId) => void;
}

export function TrackCard({ track, onPress }: TrackCardProps) {
  return (
    <Pressable onPress={() => onPress(track.id)}>
      <Text>{track.title}</Text>
    </Pressable>
  );
}
```

### File naming

- `PascalCase.tsx` for components (e.g., `TrackCard.tsx`).
- `camelCase.ts` with `use` prefix for hooks (e.g., `usePlayback.ts`).
- `camelCase.ts` for utilities (e.g., `formatDuration.ts`).
- Platform-specific files use `.ios.tsx` / `.android.tsx` suffixes.

### Imports

- Use path aliases (`@/`) — no relative path gymnastics.
- No barrel files (`index.ts` re-exports). Import directly from the source file.
- Group imports in order: `react` → `react-native` → `expo` → third-party → local.
- Use `import type` for type-only imports.

```tsx
import { useState, useCallback } from 'react';
import { View, Text, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import { useQuery } from '@tanstack/react-query';

import type { Track } from '@/features/catalog/types';
import { TrackCard } from '@/features/catalog/ui/TrackCard';
```

### General formatting

- 100 character line width.
- Trailing commas in multi-line structures.
- Single quotes for strings.
- Semicolons always.
- No `var` — use `const` by default, `let` when mutation is required.

## Patterns

### Component patterns

- **Compound components**: use `Context` + child components for complex UI (e.g., `<List>`, `<List.Item>`, `<List.Separator>`).
- **Custom hooks for shared logic**: extract reusable stateful logic into `use*` hooks, colocated in the feature's `hooks/` directory.
- **Colocation**: keep components, hooks, types, and tests close to where they are used. Move to `shared/` only when a second consumer appears.
- **Render props**: use when a component needs to delegate rendering decisions to its consumer without tightly coupling.
- **forwardRef**: use when a component needs to expose its underlying element ref to a parent (e.g., for focus management, scroll-to).

```tsx
import { forwardRef } from 'react';
import type { TextInput, TextInputProps } from 'react-native';

interface SearchInputProps extends TextInputProps {
  onSearch: (query: string) => void;
}

export const SearchInput = forwardRef<TextInput, SearchInputProps>(
  function SearchInput({ onSearch, ...props }, ref) {
    return <TextInput ref={ref} onSubmitEditing={(e) => onSearch(e.nativeEvent.text)} {...props} />;
  },
);
```

### Data fetching

- **TanStack Query for all API calls**. No raw `fetch` or `axios` in components or hooks.
- Query keys as constants, colocated with the feature's API layer.
- Use `prefetchQuery` for anticipated navigations (e.g., prefetch detail when list item is visible).
- Use optimistic updates for mutations where latency matters (save to library, toggle favorite).
- Wrap each screen in an error boundary so a single failed query doesn't crash the app.

### Error handling

- **Error boundaries at screen level**: each screen route has its own error boundary. A crash in one screen doesn't take down the app.
- **try/catch at API level**: the `shared/api-client/` layer catches and maps HTTP errors to typed `ApiError` discriminated unions. Screens consume typed errors, not raw exceptions.
- **Graceful degradation**: if non-critical data fails to load (e.g., artwork), show a placeholder — don't block the screen.
- **Never swallow errors**: every catch must either re-throw, display to the user, or log for observability. Silent `catch {}` is forbidden.

## State management

### State type table

| State type | Tool | When to use |
| --- | --- | --- |
| Client (UI) | Zustand | Theme, sidebar state, player UI, local preferences |
| Server (remote) | TanStack Query | All data from the API — tracks, playlists, search results |
| Form | React Hook Form | Multi-field forms with validation |
| Ephemeral | `useState` | Toggle, modal open/close, local input value |
| Derived | `useMemo` | Computed from other state — filtered lists, totals |

### Zustand store pattern

Stores are small, domain-focused, and colocated with the feature that owns them. Typed interface first, then implementation.

```ts
import { create } from 'zustand';

interface PlayerState {
  currentTrackId: string | null;
  isPlaying: boolean;
  play: (trackId: string) => void;
  pause: () => void;
  stop: () => void;
}

export const usePlayerStore = create<PlayerState>((set) => ({
  currentTrackId: null,
  isPlaying: false,
  play: (trackId) => set({ currentTrackId: trackId, isPlaying: true }),
  pause: () => set({ isPlaying: false }),
  stop: () => set({ currentTrackId: null, isPlaying: false }),
}));

// Selectors — use these in components to minimize re-renders
export const selectCurrentTrackId = (s: PlayerState) => s.currentTrackId;
export const selectIsPlaying = (s: PlayerState) => s.isPlaying;
```

Usage in a component:

```tsx
function NowPlaying() {
  const trackId = usePlayerStore(selectCurrentTrackId);
  const isPlaying = usePlayerStore(selectIsPlaying);
  // ...
}
```

### TanStack Query patterns

#### Query keys

Define keys as factory functions, colocated in the feature's `api/` directory:

```ts
export const trackKeys = {
  all: ['tracks'] as const,
  lists: () => [...trackKeys.all, 'list'] as const,
  list: (filters: TrackFilters) => [...trackKeys.lists(), filters] as const,
  details: () => [...trackKeys.all, 'detail'] as const,
  detail: (id: string) => [...trackKeys.details(), id] as const,
};
```

#### useQuery

```tsx
import { useQuery } from '@tanstack/react-query';
import { trackKeys } from '../api/queryKeys';
import { fetchTrack } from '../api/trackApi';

export function useTrack(trackId: string) {
  return useQuery({
    queryKey: trackKeys.detail(trackId),
    queryFn: () => fetchTrack(trackId),
    staleTime: 5 * 60 * 1000, // 5 minutes
  });
}
```

#### useMutation with optimistic update

```tsx
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { trackKeys } from '../api/queryKeys';
import { saveTrackToLibrary } from '../api/trackApi';
import type { Track } from '../types';

export function useSaveTrack() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: saveTrackToLibrary,
    onMutate: async (newTrack: Track) => {
      await queryClient.cancelQueries({ queryKey: trackKeys.lists() });

      const previous = queryClient.getQueryData<Track[]>(trackKeys.lists());
      queryClient.setQueryData<Track[]>(trackKeys.lists(), (old) =>
        old ? [...old, newTrack] : [newTrack],
      );

      return { previous };
    },
    onError: (_err, _newTrack, context) => {
      if (context?.previous) {
        queryClient.setQueryData(trackKeys.lists(), context.previous);
      }
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: trackKeys.lists() });
    },
  });
}
```

### Rules

- **Context API**: only for global, rarely-changing values (theme, locale). Not for frequently updated state.
- **No prop drilling beyond 2 levels**: if a prop passes through 2+ intermediary components that don't use it, lift to a store or context.
- **Stores are small and domain-focused**: one store per concern (player, preferences, auth). No god stores.
- **Never store derived data**: compute it with `useMemo` or a selector. If you can calculate it from existing state, don't duplicate it in the store.
- **Persist with zustand/middleware**: use `persist` middleware for state that should survive app restarts (preferences, recent searches). Define a `version` and `migrate` function for schema changes.

## Navigation

### Expo Router file conventions

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

### Typed routes and navigation

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

### Deep linking

- Define the URL scheme in `app.json` under `expo.scheme`.
- Validate incoming URLs — never trust deep link params without sanitization.
- Test with `npx uri-scheme open <url> --ios` / `--android` during development.

### Modal patterns

- Use `presentation: 'modal'` in the route's screen options for modal presentation.
- Bottom sheets: use `@gorhom/bottom-sheet` for gesture-driven bottom sheets; integrate with Expo Router by presenting the sheet from a regular screen, not as a separate route (unless it needs its own URL).

```tsx
// In _layout.tsx
<Stack.Screen name="track/[id]" options={{ presentation: 'modal' }} />
```

### Best practices

- **Minimal navigation state**: do not store navigation state in Zustand or Context. Let Expo Router own it.
- **Prefetch on visibility**: use `queryClient.prefetchQuery` when a list item becomes visible to reduce perceived latency on navigation.
- **initialRouteName**: set in the layout's `<Tabs>` or `<Stack>` to control which screen shows first without a flash.
- **Avoid deep nesting**: keep the route tree shallow. Deeply nested stacks create confusing back-button behavior and complex state.
- **Loading states on navigation**: show skeleton/loading UI in the destination screen, not a blocking spinner on the source screen.
