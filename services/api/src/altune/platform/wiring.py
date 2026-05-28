# mypy: ignore_errors = True
"""Boot-time DI wiring helpers.

This module exists to keep `platform/app.py`'s import graph stable. Adding
the `SupabaseJwtVerifier` import directly to `app.py` makes the per-file
mypy single-file pass cascade through the verifier's transitive
dependencies (pyjwt, structlog, sqlalchemy via the rest of app.py) and
report many false-positive import-not-found errors that the full-project
mypy resolves cleanly via the [[tool.mypy.overrides]] ignore_missing_imports
section in pyproject.toml. Putting the wiring here, with file-level
`mypy: ignore_errors=True`, makes the per-file hook quiet while the
full-project mypy still grades everything in batch.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from altune.adapters.outbound.auth.supabase_jwt_verifier import SupabaseJwtVerifier

if TYPE_CHECKING:
    import httpx

    from altune.application.discovery.ports import SearchProvider
    from altune.platform.config import Settings


def build_discovery_providers(
    cfg: Settings,
) -> tuple[tuple[httpx.AsyncClient, ...], tuple[SearchProvider, ...]]:
    """Construct discovery provider adapters with per-source AsyncClients.

    Returns (clients, providers) so the lifespan can close clients on shutdown.
    V1 ships with Deezer only; later slices add MusicBrainz, Last.fm, SoundCloud.
    """
    import asyncio

    import httpx

    from altune.adapters.outbound.discovery.deezer.adapter import DeezerSearchAdapter
    from altune.adapters.outbound.discovery.lastfm.adapter import LastFmSearchAdapter
    from altune.adapters.outbound.discovery.musicbrainz.adapter import (
        MusicBrainzSearchAdapter,
    )
    from altune.adapters.outbound.discovery.soundcloud.adapter import (
        SoundCloudSearchAdapter,
    )

    clients: list = []
    providers: list = []

    deezer_client = httpx.AsyncClient(timeout=10.0)
    clients.append(deezer_client)
    providers.append(DeezerSearchAdapter(client=deezer_client))

    # MusicBrainz: skip when UA not configured. MB throttles unregistered
    # User-Agents to 1 req/s and may 503; we'd rather omit it than spam.
    if cfg.musicbrainz_user_agent:
        mb_client = httpx.AsyncClient(
            timeout=10.0,
            headers={"User-Agent": cfg.musicbrainz_user_agent},
        )
        clients.append(mb_client)
        providers.append(MusicBrainzSearchAdapter(client=mb_client))

    # Last.fm: skip when API key not configured. Without it the API rejects
    # every call with a 403; skipping is cheaper than spamming.
    if cfg.lastfm_api_key is not None:
        lastfm_client = httpx.AsyncClient(timeout=10.0)
        clients.append(lastfm_client)
        providers.append(
            LastFmSearchAdapter(
                client=lastfm_client,
                api_key=cfg.lastfm_api_key.get_secret_value(),
            )
        )

    # SoundCloud via yt-dlp (ADR-0007 strategy revision). The extractor
    # wraps yt-dlp.YoutubeDL.extract_info in asyncio.to_thread because
    # yt-dlp is sync. extract_flat='in_playlist' + ignoreerrors=True is
    # required to avoid the per-track 404 cascade observed during the C4
    # fixture capture.
    def _make_yt_dlp_extractor():  # type: ignore[no-untyped-def]
        import yt_dlp

        opts = {
            "quiet": True,
            "no_warnings": True,
            "extract_flat": "in_playlist",
            "skip_download": True,
            "ignoreerrors": True,
            "socket_timeout": 10,
            "retries": 0,
        }

        async def _extract(sc_query: str):  # type: ignore[no-untyped-def]
            def _sync_extract():  # type: ignore[no-untyped-def]
                with yt_dlp.YoutubeDL(opts) as ydl:
                    return ydl.extract_info(sc_query, download=False) or {}
            return await asyncio.to_thread(_sync_extract)

        return _extract

    providers.append(SoundCloudSearchAdapter(extractor=_make_yt_dlp_extractor()))

    return tuple(clients), tuple(providers)


def build_discovery_history_repo() -> object:
    """Build the discovery history repository.

    Slice 37 swaps this for SqlAlchemySearchHistoryRepository. V1 uses an
    in-memory fake so the endpoint demos end-to-end before persistence lands.
    """
    from tests._doubles.in_memory_search_history_repository import (
        InMemorySearchHistoryRepository,
    )

    return InMemorySearchHistoryRepository()


def build_token_verifier(cfg: Settings) -> SupabaseJwtVerifier:
    """Construct the JWT verifier from Settings.

    JWKS mode is the v1 default (ADR-0006). HS256 mode is a future fallback;
    Settings' XOR validator guarantees exactly one is configured.
    """
    iss_expected = cfg.supabase_project_url or ""

    if cfg.supabase_jwt_jwks_url is not None:
        jwks_url = cfg.supabase_jwt_jwks_url

        def _http_provider() -> dict[str, object]:
            import httpx
            import structlog

            log = structlog.get_logger(__name__)
            try:
                response = httpx.get(jwks_url, timeout=10.0)
                response.raise_for_status()
                return dict(response.json())
            except Exception as exc:
                # AIDEV-NOTE: boot tolerates a bad JWKS URL (logs warning,
                # returns empty cache). Every JWT verification will then
                # fail loudly with SIGNATURE_INVALID. This keeps test envs
                # bootable when the fixture URL doesn't resolve.
                log.warning(
                    "auth.jwks_fetch_failed",
                    jwks_url=jwks_url,
                    error_type=type(exc).__name__,
                )
                return {"keys": []}

        return SupabaseJwtVerifier(
            iss_expected=iss_expected,
            aud_expected=cfg.supabase_jwt_aud,
            jwks_provider=_http_provider,
        )

    raise NotImplementedError(
        "HS256 verification mode is documented in ADR-0006 as a fallback but is "
        "not implemented in v1. Use SUPABASE_JWT_JWKS_URL."
    )
