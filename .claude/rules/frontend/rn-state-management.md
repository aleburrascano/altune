---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native state management — Zustand + TanStack Query

## State type table

| State type | Tool | When to use |
| --- | --- | --- |
| Client (UI) | Zustand | Theme, sidebar state, player UI, local preferences |
| Server (remote) | TanStack Query | All data from the API — tracks, playlists, search results |
| Form | React Hook Form | Multi-field forms with validation |
| Ephemeral | `useState` | Toggle, modal open/close, local input value |
| Derived | `useMemo` | Computed from other state — filtered lists, totals |

## Zustand store pattern

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

## TanStack Query patterns

### Query keys

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

### useQuery

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

### useMutation with optimistic update

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

## Rules

- **Context API**: only for global, rarely-changing values (theme, locale). Not for frequently updated state.
- **No prop drilling beyond 2 levels**: if a prop passes through 2+ intermediary components that don't use it, lift to a store or context.
- **Stores are small and domain-focused**: one store per concern (player, preferences, auth). No god stores.
- **Never store derived data**: compute it with `useMemo` or a selector. If you can calculate it from existing state, don't duplicate it in the store.
- **Persist with zustand/middleware**: use `persist` middleware for state that should survive app restarts (preferences, recent searches). Define a `version` and `migrate` function for schema changes.
