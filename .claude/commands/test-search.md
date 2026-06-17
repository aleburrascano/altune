# /test-search — Live search quality tester

Test discovery search queries against the running Go API and evaluate results against stated expectations. Diagnose failures by reading the relevant pipeline code.

## Usage

```
/test-search "Megamsn" → corrects to Megaman, non-empty results
/test-search "Drake" → Drake artist in top 3
/test-search "Tay-K Megaman" → Megaman by Tay-K is #1
```

The argument is one or more lines, each formatted as:
```
"<query>" → <expectation in plain English>
```

Multiple queries can be passed at once (one per line). If no argument is provided, prompt the user for queries.

## Execution steps

### 1. Authenticate

Get a Supabase JWT using the E2E fixture credentials from `apps/mobile/.env.local`:
- Read `E2E_FIXTURE_EMAIL` and `E2E_FIXTURE_PASSWORD` from that file
- Read `EXPO_PUBLIC_SUPABASE_URL` and `EXPO_PUBLIC_SUPABASE_ANON_KEY`
- POST to `{supabase_url}/auth/v1/token?grant_type=password` with the credentials
- Extract `access_token` from response

Read the Go API port from `EXPO_PUBLIC_API_URL` in `apps/mobile/.env.local`, or default to `http://localhost:8000`.

### 2. Run each query

For each query, call:
```
GET {api_url}/v1/discovery/search?q={query}&limit=10
Authorization: Bearer {token}
```

Capture the full JSON response.

### 3. Evaluate against expectations

Parse each expectation into checkable assertions. Common patterns:
- **"corrects to X"** → `corrected_query` field equals X (case-insensitive)
- **"non-empty results"** or **"results non-empty"** → `results` array length > 0
- **"X in top N"** or **"X is #N"** → result at position N-1 has title or subtitle containing X
- **"X appears"** → any result has title or subtitle containing X
- **"artist #1"** or **"artist in top 3"** → result(s) with kind="artist" at specified position(s)
- **"no results"** → results array is empty

Use judgment for fuzzy expectations. The goal is to match what the user means, not to parse a formal grammar.

### 4. Report results

For each query, report:
```
✓ "query" — PASS: [what was found]
✗ "query" — FAIL: expected [expectation], got [actual]
    Top 3 results:
    #1 [kind] "title" by "subtitle" (pop=N)
    #2 ...
    #3 ...
    corrected_query: "..." or (none)
    providers: N ok, N failed
```

### 5. On failure — diagnose

When a query fails its expectation:

1. Read the relevant pipeline code to understand WHY:
   - If correction didn't fire: read `service/correction.go` and `service/search_music.go` (preQueryCorrection / tryCorrection)
   - If wrong ranking: read `service/dedup.go` (FuseAndRank, relevanceScore, rankingKeyLess)
   - If results empty: check `sharesWord` gate, provider statuses, and whether the query norm matches any result text
   - If intent not detected: read `service/intent.go` and check vocabulary for the artist

2. Check vocabulary state:
   ```
   docker exec altune-redis-dev redis-cli GET "discovery:vocab:v1:entry:{normalized_query}"
   docker exec altune-redis-dev redis-cli ZCARD "discovery:vocab:v1:terms"
   ```

3. Report the root cause with file paths and line numbers.

### 6. Fix loop (only if user asks)

If the user says to fix it, or invokes with "fix" in the argument:
1. Propose the minimal code change
2. Apply it
3. Rebuild: `cd services/go-api && go build -o ./tmp/api.exe ./cmd/api`
4. Tell the user to restart the server
5. Re-run ALL queries (not just the failing one) to check for regressions
6. Report full results again

**CRITICAL**: Never commit fixes directly. The user will review and commit when satisfied.

## Key files to understand

- `services/go-api/internal/discovery/ARCHITECTURE.md` — pipeline flow diagram
- `services/go-api/internal/discovery/service/search_music.go` — main orchestrator
- `services/go-api/internal/discovery/service/dedup.go` — FuseAndRank, ranking, gating
- `services/go-api/internal/discovery/service/correction.go` — spelling correction
- `services/go-api/internal/discovery/service/intent.go` — artist+track detection
- `services/go-api/internal/discovery/service/query_clean.go` — noise stripping
- `services/go-api/internal/discovery/service/popularity.go` — popularity normalization
- `services/go-api/internal/discovery/service/metaphone.go` — phonetic matching
- `services/go-api/internal/discovery/adapters/cache/vocabulary_store.go` — Redis vocab (trigram + phonetic)

## Environment

- Go API: default `http://localhost:8000`, override via `EXPO_PUBLIC_API_URL` in `apps/mobile/.env.local`
- Redis: `docker exec altune-redis-dev redis-cli ...`
- Supabase: credentials in `apps/mobile/.env.local`
- Build: `cd services/go-api && go build -o ./tmp/api.exe ./cmd/api`
- Tests: `cd services/go-api && go test ./internal/discovery/... -count=1`
