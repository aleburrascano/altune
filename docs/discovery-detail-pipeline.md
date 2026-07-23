# Discovery detail / discography pipeline

> **Superseded.** This design doc has been folded into the canonical, timeless
> module architecture: [`services/go-api/internal/discovery/ARCHITECTURE.md`](../services/go-api/internal/discovery/ARCHITECTURE.md).
> Read that instead — it covers the whole discovery module (both pipelines), not
> just detail.

Section pointers for older references:

- **§6 (the rebuild / discography V2)** → ARCHITECTURE.md **§5 "The detail / discography pipeline"** (the `MergeReleases → FilterKept → verify → bucket` cores).
- **§7 (identity fracture / MB-anchored verification)** → ARCHITECTURE.md **§6 "Cross-provider identity"** (verify-on-persist, the MB anchor, cohesion).
- The two-paths framing, the provider participation matrix, the field-extraction gaps, and the open design questions all live in ARCHITECTURE.md (§0, §7, §15).
