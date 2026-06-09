# mypy: ignore_errors = True
"""HTTP exception handler registry.

Maps domain / application exceptions to HTTP responses at the inbound HTTP
boundary, keeping use cases and adapters free of HTTP status codes. Per the
adapters-layer rule, routers are thin shells — error mapping centralizes
here instead.

Slice 5: registers InvalidTokenError → 401 (auth-integration spec AC#7).
Future slices append additional mappings; do not delete existing ones.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import structlog
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from altune.application.auth.exceptions import InvalidTokenError

if TYPE_CHECKING:
    from fastapi import FastAPI, Request

_log = structlog.get_logger(__name__)


def register_exception_handlers(app: FastAPI) -> None:
    """Wire each domain/application exception type to its HTTP shape."""

    @app.exception_handler(InvalidTokenError)
    async def _invalid_token_handler(request: Request, exc: InvalidTokenError) -> JSONResponse:
        _ = request  # unused; signature matches FastAPI's handler shape
        return JSONResponse(
            status_code=401,
            content={"detail": "invalid_token", "reason": exc.reason.value},
            headers={"WWW-Authenticate": 'Bearer error="invalid_token"'},
        )

    @app.exception_handler(RequestValidationError)
    async def _validation_error_handler(
        request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        _log.warning(
            "http_validation_error",
            path=request.url.path,
            method=request.method,
            errors=exc.errors(),
        )
        return JSONResponse(status_code=422, content={"detail": exc.errors()})
