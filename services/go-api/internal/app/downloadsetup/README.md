`downloadsetup` owns how the API composes audio acquisition from configuration: which audio sources run in which tier, and how the background acquisition scheduler is built around them.

- `LiveTiers` builds the live sources the configuration turns on. The catalog pin runs first, then YouTube Music's songs search and catalog id, with yt-dlp as the fallback. It also reports whether yt-dlp is installed.
- `NewScheduler` builds the background scheduler from the tiers it is given: verification, recording resolution, fingerprinting, the upgrade lane and the Postgres job queue. Tests hand it fixture tiers to run the real pipeline offline.
- `YtDlpSearcher` and `CanaryYTMClient` build the clients the source canary probes with.
