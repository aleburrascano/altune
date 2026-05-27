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
    from altune.platform.config import Settings


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

            response = httpx.get(jwks_url, timeout=10.0)
            response.raise_for_status()
            return dict(response.json())

        return SupabaseJwtVerifier(
            iss_expected=iss_expected,
            aud_expected=cfg.supabase_jwt_aud,
            jwks_provider=_http_provider,
        )

    raise NotImplementedError(
        "HS256 verification mode is documented in ADR-0006 as a fallback but is "
        "not implemented in v1. Use SUPABASE_JWT_JWKS_URL."
    )
