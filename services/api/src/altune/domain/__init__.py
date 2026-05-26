"""Domain layer — the inner hexagon.

Pure Python only. No framework imports. No I/O. See .claude/rules/domain-layer.md.

Bounded contexts will be added as packages here when features arrive:
- catalog/   (tracks, artists, albums — the immutable identity-and-metadata side)
- library/   (user's personal collection)
- playback/  (runtime queue, current track, history)
- metadata/  (external enrichment)

Day-1 scaffold: this package is intentionally empty.
"""
