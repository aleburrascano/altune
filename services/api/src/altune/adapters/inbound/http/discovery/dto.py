"""Pydantic DTOs for the discovery HTTP routes — wire shape per spec §3.7."""

from __future__ import annotations

from datetime import datetime  # noqa: TC003
from typing import Any

from pydantic import BaseModel, ConfigDict


class SourceDto(BaseModel):
    model_config = ConfigDict(frozen=True)

    provider: str
    external_id: str
    url: str


class SearchResultDto(BaseModel):
    model_config = ConfigDict(frozen=True)

    kind: str
    title: str
    subtitle: str | None
    image_url: str | None
    confidence: str
    sources: list[SourceDto]
    extras: dict[str, Any]


class ProviderStatusDto(BaseModel):
    model_config = ConfigDict(frozen=True)

    provider: str
    status: str
    result_count: int
    latency_ms: int


class CacheDto(BaseModel):
    model_config = ConfigDict(frozen=True)

    hit: bool
    fetched_at: datetime | None


class DiscoverySearchResponse(BaseModel):
    model_config = ConfigDict(frozen=True)

    query: str
    query_norm: str
    results: list[SearchResultDto]
    providers: list[ProviderStatusDto]
    partial: bool
    cache: CacheDto


class SearchHistoryItemDto(BaseModel):
    model_config = ConfigDict(frozen=True)

    query: str
    query_norm: str
    executed_at: datetime


class DiscoverySearchHistoryResponse(BaseModel):
    model_config = ConfigDict(frozen=True)

    items: list[SearchHistoryItemDto]
    total: int


class DiscoveryClickRequest(BaseModel):
    model_config = ConfigDict(frozen=True)

    query_norm: str
    kind: str
    title: str
    subtitle: str | None = None
    position: int
    confidence: str
