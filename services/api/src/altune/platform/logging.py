"""Structured logging via structlog.

Every request carries a correlation id (added by middleware when implemented).
Logs are JSON in production, key-value pretty in development.
"""

from __future__ import annotations

import logging
from typing import cast

import structlog


def configure_logging(level: str = "INFO", *, json: bool = False) -> None:
    """Configure structlog and stdlib logging."""
    logging.basicConfig(
        format="%(message)s",
        level=getattr(logging, level.upper(), logging.INFO),
    )
    processors: list[structlog.types.Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.TimeStamper(fmt="iso"),
    ]
    if json:
        processors.append(structlog.processors.JSONRenderer())
    else:
        processors.append(structlog.dev.ConsoleRenderer())
    structlog.configure(
        processors=processors,
        wrapper_class=structlog.make_filtering_bound_logger(getattr(logging, level.upper())),
        cache_logger_on_first_use=True,
    )


def get_logger(name: str | None = None) -> structlog.stdlib.BoundLogger:
    """Return a configured structlog logger."""
    # structlog.get_logger is typed as Any in the upstream stubs; cast restores
    # the bound-logger contract for callers.
    return cast("structlog.stdlib.BoundLogger", structlog.get_logger(name))
