# UI testing workflow — agent-browser + Expo web

When making frontend changes that affect layout, navigation, or interactive behavior, **test visually before reporting done**. Use `agent-browser` (a CLI browser automation tool) with Expo's web target.

## Setup (one-time, already done)

```bash
npm install -g agent-browser
agent-browser install  # downloads Chrome
```

## Workflow

### 1. Start Expo web server (if not running)

```bash
cd apps/mobile && npx expo start --web --port 8081
```

Wait for the bundler to be ready before proceeding. The app runs at `http://localhost:8081`.

### 2. Open the app

```bash
agent-browser open http://localhost:8081
```

### 3. Take a snapshot to see interactive elements

```bash
agent-browser snapshot -i          # interactive elements with refs
agent-browser snapshot             # full accessible tree
```

### 4. Interact using refs from the snapshot

```bash
agent-browser click @e5            # click element by ref
agent-browser fill @e3 "search"    # fill an input
agent-browser type @e1 "text"      # type into focused element
```

### 5. Take screenshots to verify visual state

```bash
agent-browser screenshot           # captures current page
```

### 6. Navigate between screens

```bash
agent-browser open http://localhost:8081/library
agent-browser open http://localhost:8081/discover
```

### 7. Check specific text or element content

```bash
agent-browser get text @e1         # extract text from element
agent-browser get url              # current URL
```

## When to use

- After changing layout or styling (spacing, alignment, visibility)
- After adding new interactive elements (buttons, modals, sheets)
- After modifying navigation flows
- After changing how data renders (empty states, loading states, error states)
- When the user reports a visual bug — reproduce it first

## Limitations

- **expo-audio doesn't work on web** — scrubber/playback can't be tested this way. Use debug output + device testing for audio.
- **react-native-web rendering differs from native** — layout is close but not pixel-perfect. Good for catching structural issues, not for native-specific bugs.
- **Auth flow** — Supabase secure-store doesn't work on web. The app may redirect to sign-in. If testing authenticated screens, you may need to mock auth or use a web-compatible auth flow.

## Pattern: screenshot before and after

When fixing a visual bug, take a screenshot before your change and after to confirm the fix:

```bash
# Before
agent-browser screenshot --output before.png

# Make code changes...

# After (reload)
agent-browser open http://localhost:8081/library
agent-browser screenshot --output after.png
```
