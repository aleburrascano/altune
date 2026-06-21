# <Provider> maximization

> Status: ⬜ not audited / 🟡 partial / ✅ audited (live-probed <date>)
> Every endpoint + field below must be backed by a live probe, not memory. Mark guesses `[INFERRED]`.

## 1. Why this provider matters
What unique reach/data does it give us that others don't? (1–3 sentences.)

## 2. Access model
- Tier used (internal JSON API / official API / scraping) and base URL(s).
- Auth/key bootstrap: how we get in, where the key lives, how it rotates, how we self-heal.
- ToS posture and reach limits (public-only, region locks, etc.).

## 3. Entity model
How the provider's entities map to our `ResultKind` (track / album / artist). Note mismatches
(e.g. "every uploader is a user", "albums are typed playlists").

## 4. Endpoint catalog (verified)
| Endpoint | Returns | HTTP (probed) | Maps to port |
|---|---|---|---|

## 5. Capabilities to maximize
For each: what it gives, the endpoint, key fields, which port it feeds, risk, and status
(built / planned / deferred).

## 6. Costs & risks
Key rotation, rate limits, ToS, reach limits, ranking-gate exposure.

## 7. Current implementation state
What's wired today, where the code lives.
