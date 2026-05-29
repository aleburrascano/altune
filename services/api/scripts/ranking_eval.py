#!/usr/bin/env python
# ruff: noqa: T201  -- this is a CLI tool; print is the intended output channel.
"""Live ranking spot-check for discovery search.

Runs a query against the no-auth providers (Deezer, iTunes, MusicBrainz),
fuses with the production ranker, and prints the ranked list so you can
eyeball that the obvious match lands at position 0. This is a manual tool,
NOT part of CI — the deterministic regression guard is tests/eval/.

Usage:
    uv run python scripts/ranking_eval.py "hey jude beatles"
    uv run python scripts/ranking_eval.py "africa toto" --limit 15
"""

from __future__ import annotations

import argparse
import asyncio

import httpx

from altune.adapters.outbound.discovery.deezer.adapter import DeezerSearchAdapter
from altune.adapters.outbound.discovery.itunes.adapter import ITunesSearchAdapter
from altune.adapters.outbound.discovery.musicbrainz.adapter import MusicBrainzSearchAdapter
from altune.application.discovery.dedup import fuse_and_rank
from altune.application.discovery.normalize import normalize_for_match
from altune.domain.discovery.provider_status import ProviderStatus
from altune.domain.discovery.result_kind import ResultKind

# A contact string keeps MusicBrainz from throttling to 1 req/s for a one-off run.
_MB_USER_AGENT = "altune-ranking-eval/0.1 (https://github.com/altune; spot-check)"


async def _run(query: str, limit: int) -> None:
    kinds = frozenset({ResultKind.TRACK, ResultKind.ALBUM, ResultKind.ARTIST})
    async with (
        httpx.AsyncClient(timeout=10.0) as deezer_client,
        httpx.AsyncClient(timeout=10.0) as itunes_client,
        httpx.AsyncClient(timeout=10.0, headers={"User-Agent": _MB_USER_AGENT}) as mb_client,
    ):
        providers = [
            DeezerSearchAdapter(client=deezer_client),
            ITunesSearchAdapter(client=itunes_client),
            MusicBrainzSearchAdapter(client=mb_client),
        ]
        responses = await asyncio.gather(*(p.search(query, kinds, limit) for p in providers))

    groups = []
    for provider, resp in zip(providers, responses, strict=True):
        status = "ok" if resp.status is ProviderStatus.OK else resp.status.value
        print(f"  {provider.name:<12} {status:<8} {len(resp.results)} results")
        if resp.status is ProviderStatus.OK:
            groups.append(resp.results)

    ranked = fuse_and_rank(groups, normalize_for_match(query))
    print(f"\nFused ranking for {query!r}:")
    for i, r in enumerate(ranked[:limit]):
        srcs = "+".join(s.provider.value for s in r.sources)
        pop = r.extras.get("popularity")
        pop_s = f"pop={pop:.2f}" if isinstance(pop, (int, float)) else "pop=-"
        print(
            f"  #{i:<2} [{r.kind.value:<6}] {r.title} — {r.subtitle}  "
            f"({pop_s}; {srcs})"
        )


def main() -> None:
    parser = argparse.ArgumentParser(description="Live discovery ranking spot-check.")
    parser.add_argument("query", help="search query, e.g. 'hey jude beatles'")
    parser.add_argument("--limit", type=int, default=10, help="per-provider + display limit")
    args = parser.parse_args()
    asyncio.run(_run(args.query, args.limit))


if __name__ == "__main__":
    main()
