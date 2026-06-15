---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native Testing Library — Full API

## render & screen

```tsx
import { render, screen } from '@testing-library/react-native';

render(<MyComponent prop="value" />);

// screen is the recommended way to access queries (no destructuring)
screen.getByText('Hello');
screen.getByRole('button', { name: /submit/i });
```

`render` returns `{ unmount, rerender, toJSON, root }` but prefer `screen.*` for all queries.

`rerender(<MyComponent prop="newValue" />)` re-renders with new props without unmounting.

## Queries

### Variant table

| Variant | No match | 1 match | >1 match | Async |
|---|---|---|---|---|
| `getBy` | throw | return | throw | No |
| `getAllBy` | throw | array | array | No |
| `queryBy` | `null` | return | throw | No |
| `queryAllBy` | `[]` | array | array | No |
| `findBy` | throw | return | throw | Yes |
| `findAllBy` | throw | array | array | Yes |

- Use `getBy` when the element must exist.
- Use `queryBy` to assert an element does NOT exist (`expect(screen.queryByText('Gone')).toBeNull()`).
- Use `findBy` for elements that appear asynchronously (wraps `waitFor` + `getBy`).

### Query types (priority order)

**1. ByRole** (preferred — matches accessibility role)

```tsx
screen.getByRole('button', { name: /submit/i });
screen.getByRole('heading', { name: 'Profile' });
screen.getByRole('switch', { name: /notifications/i });
screen.getByRole('text', { name: /welcome/i });
screen.getByRole('alert');

// With state
screen.getByRole('button', { name: /save/i, disabled: true });
screen.getByRole('checkbox', { checked: true });
screen.getByRole('switch', { selected: true });
screen.getByRole('button', { expanded: false });
screen.getByRole('tab', { selected: true });
```

**2. ByText** (visible text content)

```tsx
screen.getByText('Hello World');
screen.getByText(/hello/i);         // regex, case insensitive
screen.getByText((text) => text.startsWith('Hello'));  // custom matcher
```

**3. ByLabelText** (accessibilityLabel)

```tsx
screen.getByLabelText('Play track');
screen.getByLabelText(/close/i);
```

**4. ByHintText** (accessibilityHint)

```tsx
screen.getByHintText('Navigates to settings');
```

**5. ByPlaceholderText** (TextInput placeholder)

```tsx
screen.getByPlaceholderText('Search...');
```

**6. ByDisplayValue** (current TextInput value)

```tsx
screen.getByDisplayValue('current input text');
```

**7. ByTestId** (last resort — testID prop)

```tsx
screen.getByTestId('custom-component');
```

## userEvent (recommended)

Simulates realistic user interactions with proper event sequencing. Requires `setup()`.

```tsx
import { userEvent } from '@testing-library/react-native';

const user = userEvent.setup();

// Press (fires pointerDown → pointerUp → press sequence)
await user.press(screen.getByRole('button', { name: /submit/i }));

// Long press (fires press + long press events after delay)
await user.longPress(screen.getByRole('button', { name: /options/i }));
await user.longPress(element, { duration: 1000 }); // custom duration

// Type text (fires changeText + keyPress events per character)
await user.type(screen.getByPlaceholderText('Email'), 'user@example.com');
await user.type(input, 'text', { skipPress: true }); // skip initial press

// Clear text input
await user.clear(screen.getByPlaceholderText('Email'));

// Scroll (FlashList / FlatList / ScrollView)
await user.scrollTo(screen.getByTestId('list'), { y: 500 });
await user.scrollTo(list, { x: 200, momentumY: 300 });
```

## fireEvent (low-level)

Direct event dispatch without realistic sequencing. Use `userEvent` when possible.

```tsx
import { fireEvent } from '@testing-library/react-native';

// Press
fireEvent.press(screen.getByRole('button', { name: /save/i }));

// Text change
fireEvent.changeText(screen.getByPlaceholderText('Name'), 'Alice');

// Scroll
fireEvent.scroll(screen.getByTestId('scrollview'), {
  nativeEvent: { contentOffset: { y: 200 } },
});

// Focus / blur
fireEvent(screen.getByPlaceholderText('Email'), 'focus');
fireEvent(screen.getByPlaceholderText('Email'), 'blur');

// Generic event with custom payload
fireEvent(element, 'onCustomEvent', { value: 42 });
```

## waitFor & waitForElementToBeRemoved

```tsx
import { waitFor, waitForElementToBeRemoved } from '@testing-library/react-native';

// Wait for assertion to pass (polls until timeout)
await waitFor(() => {
  expect(screen.getByText('Loaded')).toBeTruthy();
});

await waitFor(() => expect(mockFn).toHaveBeenCalled(), {
  timeout: 5000,     // max wait (default 1000ms)
  interval: 100,     // poll interval (default 50ms)
});

// Wait for element to disappear
await waitForElementToBeRemoved(() => screen.getByText('Loading...'));
```

## within (scoped queries)

```tsx
import { within } from '@testing-library/react-native';

const header = screen.getByTestId('header');
within(header).getByText('Title');
within(header).getByRole('button', { name: /back/i });

// Useful for lists with repeated content
const row = screen.getAllByRole('listitem')[0];
within(row).getByText('Track Name');
```

## renderHook

```tsx
import { renderHook, act } from '@testing-library/react-native';

const { result, rerender, unmount } = renderHook(() => useCounter(0));

expect(result.current.count).toBe(0);

act(() => {
  result.current.increment();
});

expect(result.current.count).toBe(1);

// Rerender with new props
const { result: r, rerender: rr } = renderHook(
  ({ initial }) => useCounter(initial),
  { initialProps: { initial: 0 } },
);
rr({ initial: 10 });

// With wrapper (providers)
renderHook(() => useTheme(), {
  wrapper: ({ children }) => <ThemeProvider>{children}</ThemeProvider>,
});
```

## act

```tsx
import { act } from '@testing-library/react-native';

// Synchronous state update
act(() => {
  result.current.increment();
});

// Async state update (timers, promises)
await act(async () => {
  await result.current.fetchData();
});
```

Wrap any code that triggers state updates outside of RNTL utilities in `act`. RNTL's `render`, `fireEvent`, `userEvent`, and `waitFor` already wrap in `act`.

## Testing patterns

### Button press + navigation

```tsx
it('navigates to detail on press', async () => {
  const push = jest.fn();
  jest.mocked(useRouter).mockReturnValue({ push } as any);

  render(<TrackCard track={mockTrack} />);

  const user = userEvent.setup();
  await user.press(screen.getByRole('button', { name: mockTrack.title }));

  expect(push).toHaveBeenCalledWith(`/track/${mockTrack.id}`);
});
```

### Form submission

```tsx
it('submits form with entered values', async () => {
  const onSubmit = jest.fn();
  render(<LoginForm onSubmit={onSubmit} />);

  const user = userEvent.setup();
  await user.type(screen.getByPlaceholderText('Email'), 'a@b.com');
  await user.type(screen.getByPlaceholderText('Password'), 'secret');
  await user.press(screen.getByRole('button', { name: /log in/i }));

  expect(onSubmit).toHaveBeenCalledWith({ email: 'a@b.com', password: 'secret' });
});
```

### Loading state to content

```tsx
it('shows loading then content', async () => {
  render(<TrackList />);

  expect(screen.getByText('Loading...')).toBeTruthy();

  await waitForElementToBeRemoved(() => screen.getByText('Loading...'));

  expect(screen.getByText('Track 1')).toBeTruthy();
  expect(screen.getByText('Track 2')).toBeTruthy();
});
```

### List items

```tsx
it('renders all tracks', () => {
  render(<TrackList tracks={mockTracks} />);

  const items = screen.getAllByRole('button');
  expect(items).toHaveLength(mockTracks.length);

  // Check specific item content
  const firstItem = items[0];
  within(firstItem).getByText(mockTracks[0].title);
});
```

## Rules

- **Prefer `userEvent` over `fireEvent`** — userEvent simulates real interaction sequences, fireEvent is a low-level escape hatch.
- **Query priority: `ByRole` > `ByText` > `ByLabelText` > `ByPlaceholderText` > `ByTestId`** — match how users find elements.
- **Use `screen.*` not destructured queries** — `screen` is always current after rerender.
- **No snapshot tests as primary strategy** — snapshots are brittle and don't test behavior.
- **Use `findBy*` for async content** — combines `waitFor` + `getBy`, cleaner than manual `waitFor`.
- **Assert absence with `queryBy`** — `expect(screen.queryByText('Error')).toBeNull()`.
- **One assertion per test** — focused tests pinpoint failures faster.
- **Mock at boundaries** — mock API calls (MSW), navigation, native modules. Don't mock internal component state.
