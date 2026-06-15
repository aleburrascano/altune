---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# React Native security — secrets, deep links, network, WebView, storage

## Secrets management

- NEVER hardcode API keys, tokens, or secrets in source code
- Use `expo-secure-store` (not AsyncStorage) for sensitive tokens
- `.env` files excluded from git; `.env.example` checked in as documentation
- Use EAS Secrets for CI/CD environment variables
- Runtime secrets delivered via backend API, never bundled in the client

```tsx
// GOOD — secure storage for tokens
import * as SecureStore from 'expo-secure-store';

await SecureStore.setItemAsync('auth_token', token);
const token = await SecureStore.getItemAsync('auth_token');

// BAD — AsyncStorage is not encrypted
import AsyncStorage from '@react-native-async-storage/async-storage';
await AsyncStorage.setItem('auth_token', token); // NEVER do this
```

## Deep linking

- Validate ALL incoming URLs before processing
- Whitelist allowed hosts and paths explicitly
- Sanitize URL parameters — never pass raw params to sensitive operations
- Never construct navigation routes from unvalidated external input

## Network

- HTTPS only for all network requests
- Certificate pinning for critical endpoints (auth, payments)
- Timeout all requests (15s default) — never leave requests open-ended
- Handle offline state gracefully; queue or reject operations explicitly

## WebView

- Set `originWhitelist` explicitly — never use `['*']`
- Disable JavaScript in WebViews that don't need it
- Use `onShouldStartLoadWithRequest` to intercept and validate navigation

## Input validation

- Sanitize all user input against XSS before rendering
- Validate on both client AND server — client validation is UX, server validation is security
- Use parameterized queries for any local database operations
- Limit input lengths at the component level

## Data storage

- Sensitive data (tokens, credentials, PII): `expo-secure-store`
- Non-sensitive preferences: `AsyncStorage`
- Never store PII in logs or crash reports
- Clear sensitive storage on logout
