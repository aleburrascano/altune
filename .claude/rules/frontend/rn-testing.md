---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native testing — Jest + RNTL + Detox

## Stack

- Jest + RNTL for unit/component tests
- Detox for E2E
- Target 80% lines / 70% branches

## Principles

- Test behavior not implementation
- Query by role/text/label
- One assertion per test
- Mock at boundaries
- No snapshot tests as primary strategy

## Component test example

```tsx
it('disables submit when form is invalid', () => {
  render(<MyForm />);
  const submit = screen.getByRole('button', { name: /submit/i });
  expect(submit).toBeDisabled();
});
```

## Mocking

- `expo-secure-store` — mock the module
- `expo-router` — mock navigation
- MSW for API mocking

```tsx
// expo-constants mock
jest.mock('expo-constants', () => ({
  expoConfig: { extra: { apiUrl: 'http://test' } },
}));

// expo-router mock
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), back: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

// expo-image mock
jest.mock('expo-image', () => ({
  Image: 'Image',
}));
```

## File organization

- Tests adjacent to source (`__tests__/` dirs)
- Shared utils in `tests/helpers/`
- E2E in `e2e/`

## Detox with EAS Build

```json
{
  "build": {
    "test": {
      "ios": { "simulator": true },
      "android": { "gradleCommand": ":app:assembleDebug :app:assembleAndroidTest" },
      "env": { "DETOX_CONFIG": ".detoxrc.js" }
    }
  }
}
```

- Use `expo-dev-client` for native module testing
