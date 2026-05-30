"""Discovery search batch evaluation harness.

A dev tool (outside src/, outside the hexagonal layers) that runs hundreds of
real queries against the live provider stack with ground truth, and classifies
where the search fails: ranking failures (our bug) vs coverage ceilings (no
provider has the track). Measure-first; tuning follows from the report.

See docs / the session plan for the design. Entry points:
    uv run python -m scripts.discovery_eval.snapshot_library   # one-time
    uv run python -m scripts.discovery_eval.run_eval           # batch run
"""
