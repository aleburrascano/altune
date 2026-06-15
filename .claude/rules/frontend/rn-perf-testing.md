---
paths:
  - "apps/mobile/**/*.ts"
  - "apps/mobile/**/*.tsx"
---

# Reassure — performance regression testing

`reassure` measures render counts and render duration of React Native components, detecting performance regressions before they ship. Built on top of RNTL.

## measureRenders

Measures how many times a component renders and how long each render takes.

```tsx
import { measureRenders } from 'reassure';

test('TrackCard renders efficiently', async () => {
  await measureRenders(<TrackCard track={mockTrack} onPress={jest.fn()} />);
});

// With interaction scenario
test('TrackList handles scroll without excessive re-renders', async () => {
  const scenario = async (screen: RenderAPI) => {
    const user = userEvent.setup();
    await user.scrollTo(screen.getByTestId('track-list'), { y: 500 });
  };

  await measureRenders(<TrackList tracks={mockTracks} />, { scenario });
});
```

### Options

| Option | Type | Default | Description |
|---|---|---|---|
| `runs` | `number` | `10` | Number of measurement iterations |
| `warmupRuns` | `number` | `1` | Warmup iterations (excluded from results) |
| `scenario` | `(screen) => Promise<void>` | — | User interaction to measure |
| `wrapper` | `React.ComponentType` | — | Provider wrapper (QueryClient, Theme, etc.) |
| `writeFile` | `boolean` | `true` | Write results to `.reassure/` output |

```tsx
await measureRenders(<HeavyComponent />, {
  runs: 20,
  warmupRuns: 3,
  wrapper: ({ children }) => (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>{children}</ThemeProvider>
    </QueryClientProvider>
  ),
  scenario: async (screen) => {
    await userEvent.setup().press(screen.getByRole('button', { name: /load/i }));
  },
});
```

## measureFunction

Measures execution time of a synchronous function.

```tsx
import { measureFunction } from 'reassure';

test('filterTracks is fast for large lists', async () => {
  await measureFunction(() => filterTracks(largeMockList, { genre: 'rock' }));
});

test('sortTracks handles 10k items', async () => {
  await measureFunction(() => sortTracks(tenThousandTracks, 'title', 'asc'), {
    runs: 50,
  });
});
```

## measureAsyncFunction

Measures execution time of an async function.

```tsx
import { measureAsyncFunction } from 'reassure';

test('parsePlaylist completes quickly', async () => {
  await measureAsyncFunction(async () => {
    await parsePlaylistFile(largePlaylistBlob);
  });
});
```

## Running

### Local

```bash
# Run perf tests and generate baseline
npx reassure --baseline

# Run perf tests and compare against baseline
npx reassure
```

Results are written to `.reassure/output.json` (current) and `.reassure/baseline.json`.

### CI

```bash
# On main branch (generate baseline)
npx reassure --baseline

# On PR branch (compare)
npx reassure
```

## CI integration

### GitHub Actions

```yaml
name: Performance
on: [pull_request]

jobs:
  perf:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Install dependencies
        run: npm ci

      - name: Generate baseline on base branch
        run: |
          git checkout ${{ github.base_ref }}
          npx reassure --baseline

      - name: Compare on PR branch
        run: |
          git checkout ${{ github.head_ref }}
          npx reassure

      - name: Post results
        uses: callstack/reassure-action@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
```

### Danger.js

```ts
// dangerfile.ts
import { dangerReassure } from 'reassure';

dangerReassure({
  inputFilePath: '.reassure/output.json',
  baselineFilePath: '.reassure/baseline.json',
});
```

Posts a formatted comparison table as a PR comment with pass/fail indicators.

## Output categories

| Category | Meaning | Action |
|---|---|---|
| **Significant changes** | Render count or duration changed beyond threshold | Investigate — likely a real regression or improvement |
| **Meaningless changes** | Changes within noise margin | Safe to ignore |
| **Count changed** | Render count increased or decreased | Check if new renders are expected |
| **Added** | New test with no baseline | Becomes baseline for future comparisons |
| **Removed** | Baseline test no longer present | Clean up if intentional |

## Configuration

```js
// reassure.config.js
module.exports = {
  testMatch: '**/*.perf-test.{ts,tsx}',
  runs: 10,
  outputFile: '.reassure/output.json',
};
```

## Rules

- **Run on the same hardware** — performance numbers are machine-dependent. CI runners should be consistent (same instance type, no shared runners for baselines vs. comparisons).
- **Use `.perf-test.tsx` extension** — separates performance tests from unit tests; allows running them independently with different Jest config or CI triggers.
- **Mock external calls** — network, storage, and native modules add variance. Mock them for stable measurements.
- **Measure interactions, not just initial render** — use `scenario` to simulate real user flows (scroll, press, type). Initial render alone misses re-render regressions.
- **Set `runs` high enough** — 10 is the default minimum. For noisy components, increase to 20-50 for statistical confidence.
- **Don't optimize prematurely** — add perf tests for components you've identified as bottlenecks (lists, dashboards, complex forms), not every component.
- **Commit baseline to git** — store `.reassure/baseline.json` so CI can compare without regenerating from main every time.
- **Review count changes carefully** — a render count increase from 2 to 4 may indicate a missing `React.memo` or unnecessary state update that passed through code review unnoticed.
