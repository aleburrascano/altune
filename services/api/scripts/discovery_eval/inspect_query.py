# mypy: ignore_errors = True
# ruff: noqa: T201  -- CLI tool; print is the intended output channel.
"""Inspect one discovery query in full detail.

Runs a single query through the real stack (prod-equivalent execute()) and
prints, for every ranked result, the signals the ranker sorts on: relevance
band, popularity, demotion flag, multi-source, prior, and which providers
returned it. Use it to see *why* a wrong result outranks the right one.

Usage:
    uv run python -m scripts.discovery_eval.inspect_query "blinding lights the weeknd"
"""

from __future__ import annotations

import argparse
import asyncio
from uuid import UUID

from tests._doubles.in_memory_search_history_repository import (
    InMemorySearchHistoryRepository,
)

from altune.application.discovery.dedup import (
    _popularity,
    _providers_of,
    _relevance_score,
)
from altune.application.discovery.quality_scorer import is_demoted as _is_demoted
from altune.application.discovery.normalize import normalize_for_match
from altune.application.discovery.search_music import SearchMusic, SearchMusicInput
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.shared.user_id import UserId
from altune.platform.config import Settings
from altune.platform.wiring import build_discovery_providers

_USER = UserId(UUID("00000000-0000-0000-0000-000000000001"))


async def _run(query: str, limit: int) -> None:
    cfg = Settings()
    clients, providers = build_discovery_providers(cfg)
    providers = [p for p in providers if p.name != "soundcloud"]
    popularity = next((p for p in providers if p.name == "lastfm"), None)
    artwork = next((p for p in providers if p.name == "deezer"), None)
    use_case = SearchMusic(
        providers=providers,
        history_repo=InMemorySearchHistoryRepository(),
        per_source_timeout_s=8.0,
        popularity_resolver=popularity,
        artwork_resolver=artwork,
    )
    out = await use_case.execute(
        SearchMusicInput(
            raw_query=query,
            user_id=_USER,
            kinds=frozenset(ResultKind),
            limit=limit,
        )
    )
    for c in clients:
        await c.aclose()

    qn = normalize_for_match(query)
    print(f"\nquery={query!r}  norm={qn!r}")
    print(
        f"providers: {[(p.provider_name, p.status.value, p.result_count) for p in out.providers]}"
    )
    print(
        f"\n{'#':>2} {'kind':6} {'rel':>4} {'band':>4} {'pop':>4} {'dem':>3} {'ms':>2} {'rrf':>6}  title — subtitle  [providers]"
    )
    for i, r in enumerate(out.results[:limit]):
        rel = _relevance_score(r, qn)
        multi = 1 if len(_providers_of(r)) > 1 else 0
        rrf = float(r.extras.get("_rrf", 0.0))
        print(
            f"{i:>2} {r.kind.value:6} {rel:>4.2f} {round(rel, 1):>4.1f} "
            f"{_popularity(r):>4.2f} {int(_is_demoted(r)):>3} {multi:>2} {rrf:>6.4f}  "
            f"{r.title} — {r.subtitle}  [{'+'.join(s.provider.value for s in r.sources)}]"
        )


def main() -> None:
    p = argparse.ArgumentParser(description="Inspect one discovery query's ranking.")
    p.add_argument("query")
    p.add_argument("--limit", type=int, default=15)
    args = p.parse_args()
    asyncio.run(_run(args.query, args.limit))


if __name__ == "__main__":
    main()
