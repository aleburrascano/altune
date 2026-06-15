---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native component patterns — data fetching, error handling

## Component patterns

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

## Data fetching

- **TanStack Query for all API calls**. No raw `fetch` or `axios` in components or hooks.
- Query keys as constants, colocated with the feature's API layer.
- Use `prefetchQuery` for anticipated navigations (e.g., prefetch detail when list item is visible).
- Use optimistic updates for mutations where latency matters (save to library, toggle favorite).
- Wrap each screen in an error boundary so a single failed query doesn't crash the app.

## Error handling

- **Error boundaries at screen level**: each screen route has its own error boundary. A crash in one screen doesn't take down the app.
- **try/catch at API level**: the `shared/api-client/` layer catches and maps HTTP errors to typed `ApiError` discriminated unions. Screens consume typed errors, not raw exceptions.
- **Graceful degradation**: if non-critical data fails to load (e.g., artwork), show a placeholder — don't block the screen.
- **Never swallow errors**: every catch must either re-throw, display to the user, or log for observability. Silent `catch {}` is forbidden.
