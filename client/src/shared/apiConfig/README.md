`apiConfig` owns where the app finds go-api and how long a request may wait.

The app's composition root, `src/mobile/RootLayout.tsx`, reads `EXPO_PUBLIC_API_URL` at startup and hands it to `installApiBase`, which validates it. Until something installs a base, `apiBase()` returns the loopback development default, which is what tests use.
