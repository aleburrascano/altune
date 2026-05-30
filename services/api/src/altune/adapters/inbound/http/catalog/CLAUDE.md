# adapters/inbound/http/catalog — local context

FastAPI router for the catalog context. Thin shells: parse → call use case → serialize. No business logic (enforced by `.claude/rules/adapters-layer.md`).

## Files

- **router.py** — `APIRouter(prefix="/v1")`.
  - `GET /tracks` → `ListTracks`. Paginated; `limit`/`offset` bounds give 422.
  - `POST /tracks` → `AddTrackToLibrary`. Returns **201** on a fresh save, **200** on a dedup hit (set via the injected `Response`), with an `http_post_tracks_dedup_hit` log line as the dedup telemetry (spec Telemetry S6). Commits the session inside the handler (the use case is the unit-of-work boundary).
  - Both build `TrackResponse` from domain `Track` values — `TrackRow` never leaves the persistence layer.
- **dto.py** — frozen Pydantic boundary models.
  - `CreateTrackRequest(title, artist, album?, duration_seconds?, artwork_url?)` — POST body.
  - `TrackResponse` — includes `acquisition_status` (`AcquisitionStatus.value`) + `artwork_url` so `pending` and cover art survive a library refetch.
  - `ListTracksResponse` — page envelope (`items`, `total`, `limit`, `offset`, `has_more`).

## Conventions

- No `deps.py` here — the router wires its use case inline from `request.app.state.sessionmaker` (set up by the `platform/app.py` lifespan). `user_id` comes from the `current_user_id` auth dependency.
- Route coverage lives in `tests/e2e/test_tracks_route.py` (e2e via `TestClient`), not integration.
