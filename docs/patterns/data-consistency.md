# Data Consistency Contract

Every service, bounded context, and feature that manages data with external dependencies (files on disk, external APIs, other services) must implement this cascade pattern.

## The rule

When a data inconsistency is detected at any layer, the detecting layer is responsible for:

1. **Fix** — correct the inconsistency in its own store (e.g., mark track as failed, delete orphan).
2. **Log** — emit a structured log event with `entity_id`, `reason`, and `user_id`.
3. **Signal** — notify downstream consumers so they can update (cache invalidation, UI refresh, event emission).

## Examples

### Audio file missing (backend → mobile)

1. Stream handler detects file not on disk.
2. **Fix**: `ReconcileTrackStatus` use case marks track as `failed`, clears `audio_ref`.
3. **Log**: `track_marked_failed` event with track_id, reason, user_id.
4. **Signal**: HTTP 404 response. Mobile's playback timeout fires → invalidates library cache → UI refreshes.

### Duplicate tracks (migration)

1. Dedup migration detects rows with same `dedup_key` per user.
2. **Fix**: keep best copy (ready > pending > failed, earliest added_at), delete others, remap playlists.
3. **Log**: `dedup_migration_completed` with counts.
4. **Signal**: next library fetch returns deduplicated data.

### Track deleted by user (mobile → backend)

1. User long-presses track → "Remove from Library".
2. **Fix**: `DELETE /v1/tracks/{id}` removes from DB.
3. **Log**: `track_deleted_by_user` event.
4. **Signal**: mobile invalidates library + playlist caches.

## For new features

When adding a feature that creates, modifies, or depends on data:

- Ask: "What happens if this data disappears or becomes inconsistent?"
- Implement detection (reactive on access, or proactive via health check).
- Implement the three-step cascade (fix, log, signal).
- Add the scenario to this document.

## Related

- `docs/specs/resilience-v1/spec.md` — the spec that introduced this contract.
- [vault: wiki/concepts/Self-Healing Systems.md] — the advanced roadmap.
- [vault: wiki/concepts/Reactive Architecture.md] — the message-driven foundation.
