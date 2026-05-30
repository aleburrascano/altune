# mypy: ignore_errors = True
# ruff: noqa: T201  -- CLI tool; print is the intended output channel.
"""Batch evaluation runner for discovery search.

Runs the corpus through the REAL provider stack in-process (bypassing HTTP /
auth) and classifies every query: HIT@1 / HIT@3 / FOUND_LOW (ranking is fine
or marginal) vs RANKING_FAILURE (a provider returned it, the ranker buried it —
our bug) vs COVERAGE_CEILING (no provider has it — a catalog gap).

To judge coverage without doubling external calls, each provider is wrapped in
a RecordingProvider that captures the raw results of the very same `search()`
calls `SearchMusic.execute()` makes internally.

Usage:
    uv run python -m scripts.discovery_eval.run_eval                  # full run
    uv run python -m scripts.discovery_eval.run_eval --max-queries 20 # smoke
    uv run python -m scripts.discovery_eval.run_eval --providers deezer,itunes
"""

from __future__ import annotations

import argparse
import asyncio
import json
from collections import Counter
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING
from uuid import UUID

from tests._doubles.in_memory_search_history_repository import (
    InMemorySearchHistoryRepository,
)

from altune.application.discovery.search_music import (
    SearchMusic,
    SearchMusicInput,
)
from altune.domain.discovery.result_kind import ResultKind
from altune.domain.shared.user_id import UserId
from altune.platform.config import Settings
from altune.platform.wiring import build_discovery_providers
from scripts.discovery_eval import corpus as corpus_mod
from scripts.discovery_eval import score as score_mod
from scripts.discovery_eval.score import SimpleResult

if TYPE_CHECKING:
    from altune.application.discovery.ports import ProviderSearchResponse, SearchProvider

_DATA_DIR = Path(__file__).parent / "data"
_ALL_KINDS = frozenset(ResultKind)
_EVAL_USER = UserId(UUID("00000000-0000-0000-0000-000000000001"))

# Minimum seconds between consecutive calls to each provider, so the harness
# stays inside each one's published rate budget instead of hammering it into
# rate-limit errors (which fake "no provider has it" coverage ceilings).
# MusicBrainz ~1 req/s; iTunes ~20/min; TheAudioDB free key ~30/min; Deezer
# and Last.fm are generous. Tuned empirically against the rate-limit tallies.
_PACE_DEFAULTS = {
    "musicbrainz": 1.1,
    "itunes": 3.0,
    "theaudiodb": 2.0,
    "deezer": 0.1,
    "lastfm": 0.25,
    "soundcloud": 1.0,
}


class _Pacer:
    """Spaces calls so consecutive ones are at least `interval` seconds apart.

    Shared across all queries for a given provider; the lock serializes that
    provider's calls while letting other providers run concurrently.
    """

    def __init__(self, interval: float) -> None:
        self._interval = interval
        self._lock = asyncio.Lock()
        self._last = 0.0

    async def wait(self) -> None:
        async with self._lock:
            loop = asyncio.get_running_loop()
            delay = self._interval - (loop.time() - self._last)
            if delay > 0:
                await asyncio.sleep(delay)
            self._last = asyncio.get_running_loop().time()


@dataclass
class _RecordingProvider:
    """Wraps a SearchProvider: paces the call, then captures its raw results."""

    inner: SearchProvider
    captured: list[SearchResultLike]
    pacer: _Pacer | None = None

    @property
    def name(self) -> str:
        return self.inner.name

    async def search(
        self, query: str, kinds: frozenset[ResultKind], limit: int
    ) -> ProviderSearchResponse:
        if self.pacer is not None:
            await self.pacer.wait()
        resp = await self.inner.search(query, kinds, limit)
        self.captured.extend(_to_simple(r) for r in resp.results)
        return resp

    async def lookup_by_url(self, url: str):  # type: ignore[no-untyped-def]
        return await self.inner.lookup_by_url(url)


# A SearchResult-ish row; we only reach for kind/title/subtitle.
SearchResultLike = SimpleResult


def _to_simple(result: object) -> SimpleResult:
    kind = result.kind
    title = result.title
    subtitle = getattr(result, "subtitle", None)
    kind_str = kind.value if hasattr(kind, "value") else str(kind)
    return SimpleResult(kind=kind_str, title=title, subtitle=subtitle)


def _select_providers(
    providers: tuple[SearchProvider, ...],
    only: set[str] | None,
    include_soundcloud: bool,
) -> list[SearchProvider]:
    out = []
    for p in providers:
        if p.name == "soundcloud" and not include_soundcloud:
            continue
        if only is not None and p.name not in only:
            continue
        out.append(p)
    return out


async def _run_one(
    base_providers: list[SearchProvider],
    pacers: dict[str, _Pacer],
    popularity_resolver: object,
    artwork_resolver: object,
    query: corpus_mod.EvalQuery,
    limit: int,
    timeout_s: float,
) -> tuple[score_mod.QueryOutcome, list[str]]:
    """Run a single query through execute(), capturing raw coverage + statuses."""
    raw: list[SimpleResult] = []
    wrapped = [
        _RecordingProvider(inner=p, captured=raw, pacer=pacers.get(p.name)) for p in base_providers
    ]
    use_case = SearchMusic(
        providers=wrapped,
        history_repo=InMemorySearchHistoryRepository(),
        per_source_timeout_s=timeout_s,
        popularity_resolver=popularity_resolver,  # type: ignore[arg-type]
        artwork_resolver=artwork_resolver,  # type: ignore[arg-type]
    )
    output = await use_case.execute(
        SearchMusicInput(
            raw_query=query.query,
            user_id=_EVAL_USER,
            kinds=_ALL_KINDS,
            limit=limit,
        )
    )
    ranked = [_to_simple(r) for r in output.results]
    outcome = score_mod.classify(query, ranked, raw=raw)
    statuses = [f"{p.provider_name}:{p.status.value}" for p in output.providers]
    return outcome, statuses


async def _run_all(args: argparse.Namespace) -> None:
    cfg = Settings()
    clients, all_providers = build_discovery_providers(cfg)
    only = {s.strip() for s in args.providers.split(",")} if args.providers else None
    base = _select_providers(all_providers, only, args.soundcloud)
    if not base:
        raise SystemExit("No providers selected.")
    popularity = next((p for p in all_providers if p.name == "lastfm"), None)
    artwork = next((p for p in all_providers if p.name == "deezer"), None)
    pacers = {p.name: _Pacer(_PACE_DEFAULTS.get(p.name, 0.5)) for p in base}
    print(f"Providers: {', '.join(p.name for p in base)}")
    print(f"Pacing (s/call): {{{', '.join(f'{n}:{pc._interval}' for n, pc in pacers.items())}}}")
    if popularity is None:
        print("  (no Last.fm key — popularity enrichment OFF; ranking differs from prod)")

    # Build the corpus: library (sampled) + mainstream (full), unless restricted.
    queries: list[corpus_mod.EvalQuery] = []
    if args.source in ("both", "mainstream"):
        queries += corpus_mod.build_corpus(
            corpus_mod.MAINSTREAM, max_queries=10_000, seed=args.seed, source="mainstream"
        )
    if args.source in ("both", "library"):
        library = corpus_mod.load_library(_DATA_DIR / "library.json")
        remaining = max(0, args.max_queries - len(queries))
        queries += corpus_mod.build_corpus(library, max_queries=remaining, seed=args.seed)
    queries = queries[: args.max_queries]
    print(
        f"Running {len(queries)} queries (concurrency={args.concurrency}, timeout={args.timeout}s)..."
    )

    sem = asyncio.Semaphore(args.concurrency)
    status_tally: Counter[str] = Counter()
    done = 0

    async def _guarded(q: corpus_mod.EvalQuery) -> score_mod.QueryOutcome:
        nonlocal done
        async with sem:
            outcome, statuses = await _run_one(
                base, pacers, popularity, artwork, q, args.limit, args.timeout
            )
        status_tally.update(statuses)
        done += 1
        if done % 25 == 0 or done == len(queries):
            print(f"  {done}/{len(queries)}")
        return outcome

    outcomes = await asyncio.gather(*(_guarded(q) for q in queries))
    for c in clients:
        await c.aclose()

    _write_report(args, outcomes, base, status_tally)


def _write_report(
    args: argparse.Namespace,
    outcomes: list[score_mod.QueryOutcome],
    providers: list[SearchProvider],
    status_tally: Counter[str],
) -> None:
    label = args.label or datetime.now(UTC).strftime("%Y%m%d_%H%M%S")
    agg = score_mod.aggregate(outcomes)
    _DATA_DIR.mkdir(parents=True, exist_ok=True)

    # JSON: machine-readable full dump.
    json_path = _DATA_DIR / f"report_{label}.json"
    json_path.write_text(
        json.dumps(
            {
                "label": label,
                "providers": [p.name for p in providers],
                "n": len(outcomes),
                "aggregate": agg,
                "provider_status_tally": dict(status_tally),
                "outcomes": [
                    {
                        "query": o.query,
                        "category": o.category,
                        "source": o.source,
                        "expected": o.expected_label,
                        "classification": o.classification,
                        "rank": o.rank,
                        "top5": list(o.top5),
                    }
                    for o in outcomes
                ],
            },
            ensure_ascii=False,
            indent=1,
        ),
        encoding="utf-8",
    )

    # Markdown: human-readable summary + failure tables.
    lines: list[str] = [
        f"# Discovery eval report — {label}",
        "",
        f"- Queries: **{len(outcomes)}**",
        f"- Providers: {', '.join(p.name for p in providers)}",
        f"- Provider status tally: {dict(status_tally)}",
        "",
        "## Aggregate metrics by category",
        "",
        "| category | n | hit@1 | hit@3 | MRR | coverage | ranking-fail |",
        "|---|--:|--:|--:|--:|--:|--:|",
    ]
    order = ["__all__", *sorted(k for k in agg if k != "__all__")]
    for cat in order:
        m = agg[cat]
        lines.append(
            f"| {cat} | {int(m['n'])} | {m['hit@1']:.2f} | {m['hit@3']:.2f} | "
            f"{m['mrr']:.2f} | {m['coverage']:.2f} | {m['ranking_fail_rate']:.2f} |"
        )

    def _section(title: str, klass: str) -> None:
        rows = [o for o in outcomes if o.classification == klass]
        lines.extend(["", f"## {title} ({len(rows)})", ""])
        if not rows:
            lines.append("_none_")
            return
        lines.append("| query | expected | top-5 returned |")
        lines.append("|---|---|---|")
        for o in rows:
            top = "<br>".join(o.top5) or "—"
            lines.append(f"| `{o.query}` | {o.expected_label} | {top} |")

    # Ranking failures first — these are the actionable bugs.
    _section(
        "RANKING FAILURES (actionable — provider had it, ranker buried it)",
        score_mod.RANKING_FAILURE,
    )
    _section("FOUND BUT LOW (rank ≥ 3)", score_mod.FOUND_LOW)
    _section(
        "COVERAGE CEILINGS (no provider returned it — catalog gap, not a ranker bug)",
        score_mod.COVERAGE_CEILING,
    )

    md_path = _DATA_DIR / f"report_{label}.md"
    md_path.write_text("\n".join(lines), encoding="utf-8")

    # Console summary.
    a = agg["__all__"]
    print("\n=== SUMMARY ===")
    print(
        f"n={int(a['n'])}  hit@1={a['hit@1']:.2f}  hit@3={a['hit@3']:.2f}  "
        f"MRR={a['mrr']:.2f}  coverage={a['coverage']:.2f}  "
        f"ranking_fail={a['ranking_fail_rate']:.2f}"
    )
    print(f"Report: {md_path}")
    print(f"JSON:   {json_path}")


def main() -> None:
    p = argparse.ArgumentParser(description="Discovery search batch eval.")
    p.add_argument("--max-queries", type=int, default=400)
    p.add_argument("--seed", type=int, default=42)
    p.add_argument("--concurrency", type=int, default=3)
    p.add_argument("--limit", type=int, default=25, help="results requested per search")
    p.add_argument(
        "--timeout",
        type=float,
        default=8.0,
        help="per-source timeout; must exceed the pacing wait (coverage mode, not prod's 1.5)",
    )
    p.add_argument("--providers", type=str, default=None, help="csv subset, e.g. deezer,itunes")
    p.add_argument("--soundcloud", action="store_true", help="include SoundCloud (slow yt-dlp)")
    p.add_argument("--source", choices=["both", "library", "mainstream"], default="both")
    p.add_argument("--label", type=str, default=None, help="report filename label")
    args = p.parse_args()
    asyncio.run(_run_all(args))


if __name__ == "__main__":
    main()
