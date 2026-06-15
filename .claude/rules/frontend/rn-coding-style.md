---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native coding style — TypeScript and component conventions

## TypeScript

- Strict mode always (`strict: true`). No `any` — use `unknown` + type guards.
- Prefer `interface` for object shapes, `type` for unions and intersections.
- Use `as const` over enums for literal types.
- Use `import type` for type-only imports.
- Discriminated unions for state machines (loading / loaded / error).

## Components

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

## File naming

- `PascalCase.tsx` for components (e.g., `TrackCard.tsx`).
- `camelCase.ts` with `use` prefix for hooks (e.g., `usePlayback.ts`).
- `camelCase.ts` for utilities (e.g., `formatDuration.ts`).
- Platform-specific files use `.ios.tsx` / `.android.tsx` suffixes.

## Imports

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

## General formatting

- 100 character line width.
- Trailing commas in multi-line structures.
- Single quotes for strings.
- Semicolons always.
- No `var` — use `const` by default, `let` when mutation is required.
