`downloadfromlibrary` owns how download's ports are served by library.

It translates only: each type here implements one acquisition port on a catalog service, so neither feature imports the other.

- `NewStoredAudioReader` serves acquisition's stored-audio reader from catalog's audio store, copying a stored file into a working directory so acquisition can check it again.
- `NewTrackLedger` serves acquisition's track ledger from catalog's track repository. It reads a track as acquisition's snapshot, and writes a completion, failure or revert through catalog's track rules under optimistic locking, refusing a write whose acquisition lease was taken over.
- `NewOrphanedAudioRecorder` serves acquisition's orphaned-audio recorder from catalog's orphaned-audio queue.

`job_queue_fences_catalog_writes_integration_test.go` proves, on Postgres, how acquisition's job queue fences catalog's track writes. An enqueue, claim, reclaim or retry makes a catalog write loaded before it conflict. A catalog write that rereads afterwards still lands, and so does one loaded while a run heartbeats, releases and settles. Two races are covered too: an enqueue racing a catalog write ends pending, and a claim racing one keeps the start time.
