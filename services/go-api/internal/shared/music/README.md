Shared kernel: `music` owns the vocabulary search, library and download agree on about a recording: its identity, title markers, length, text matching, file-host links and how a download ended.

- Identity: `RecordingIdentity` is the one value that says which recording a result or a saved track is. Search produces it and download consumes it whole, so a fact search knows can never be dropped at a bridge. `CanonicalISRC`, `SplitCredit` and `PlainNameKey` give the canonical form of ISRCs, credits and song names.
- Title markers: `ParseTags`, `VersionLabels`, `BaseTitle` and the other title functions say which version tags a track title carries.
- Length: `TrackLength` says what a plausible track length is, and when two lengths are the same recording. A plausible length is a finite number of seconds from 0 to 7 days. 0 means the length is unknown. `TrackLengthOf` refuses anything else with `ErrImplausibleTrackLength`. Two known lengths are the same recording when they differ by at most 5 s or 3% of the expected length, whichever is larger. An unknown length is never the same recording as anything. Wire formats and database columns keep plain seconds, and adapters convert at the edge.
- Text: `NormalizeForMatch`, `NormalizeForIdentity`, `MatchKey`, `TokenSortRatio` and `LevenshteinDistance` say how text is normalised and fuzzily compared.
- File hosts: `ParseFileLink`, `IsFileHost`, `ValidFileHostID`, `FileHostDownloadURL` and `FileHostPageFile` say which file-host links are valid and where they download from.
- Download outcome: `FailureCode` and `FailureMessage` are the failure codes a download can end with and the message a person sees for each. `AcquisitionProvenance` is how a downloaded track's audio was checked, as the value stored with the track.

It does no I/O and imports no module.
