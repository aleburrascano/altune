`downloadfromlibrary` owns how download's ports are served by library.

It translates only: each type here implements one acquisition port on a catalog service, so neither feature imports the other.

- `NewStoredAudioReader` serves acquisition's stored-audio reader from catalog's audio store, copying a stored file into a working directory so acquisition can check it again.
- `NewLibraryTracks` serves the library half of acquisition's track ledger (`download.TrackLibrary`) from catalog's track repository. It reads a track as acquisition's snapshot, and writes a completion, failure or revert through catalog's track rules under optimistic locking, refusing a write whose acquisition lease was taken over. It answers `EvidenceAccepted`, whether the track accepts a completion's confidence and evidence, because that is catalog's rule. It knows nothing of download's upgrade state: `download.New` joins its own `acquisition_upgrade_state` to this half.
- `NewInPlaceVerdicts` serves the library half of acquisition's in-place recorder (`download.InPlaceVerdicts`): it hands a verdict to catalog's track repository and says whether it was recorded. `download.New` writes the in-place state after it.
- `Rules` serves the status rules from catalog's acquisition transitions.
- `NewOrphanedAudioRecorder` serves acquisition's orphaned-audio recorder from catalog's orphaned-audio queue.

`job_queue_fences_catalog_writes_integration_test.go` proves, on Postgres, how acquisition's job queue fences catalog's track writes. An enqueue, claim, reclaim or retry makes a catalog write loaded before it conflict. A catalog write that rereads afterwards still lands, and so does one loaded while a run heartbeats, releases and settles. Two races are covered too: an enqueue racing a catalog write ends pending, and a claim racing one keeps the start time. `in_place_recorder_integration_test.go` proves the joined recorder on Postgres.
