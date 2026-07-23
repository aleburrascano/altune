package service

// Record-type normalization + bucketing — build step 3 of the discography
// rebuild (doc §6). Providers under-label record_type (iTunes never set it,
// Spotify/Apple only flag "single"), which is why album/single/EP grouping was
// "better but not perfect". After best-of merge has folded every provider's
// signal into one record_type, this applies the final rules and buckets. Pure.

// NormalizeRecordType returns the final bucket label for a merged release:
// album | single | ep. The rules, in order:
//   - A KNOWN one-track release is a single, whatever a provider called it (the
//     "if there's only one song, it's a single" rule) — a 1-track "album" is the
//     common mislabel.
//   - Otherwise trust the merged record_type (single/ep, or album; compilation
//     folds into album for display).
//   - Unknown → album (the safe default bucket).
func NormalizeRecordType(m MergedRelease) string {
	if m.Result.TrackCount == 1 {
		return "single"
	}
	switch stringExtra(m.Result.Extras, "record_type") {
	case "single":
		return "single"
	case "ep":
		return "ep"
	default: // album, compilation, or unknown
		return "album"
	}
}

// DiscographyBuckets is the sectioned discography the detail screen renders:
// Albums (incl. compilations), Singles, EPs. Order within each is the caller's
// (chronological).
type DiscographyBuckets struct {
	Albums  []MergedRelease
	Singles []MergedRelease
	EPs     []MergedRelease
}

// BucketDiscography partitions releases by their normalized record type.
func BucketDiscography(releases []MergedRelease) DiscographyBuckets {
	var b DiscographyBuckets
	for _, m := range releases {
		switch NormalizeRecordType(m) {
		case "single":
			b.Singles = append(b.Singles, m)
		case "ep":
			b.EPs = append(b.EPs, m)
		default:
			b.Albums = append(b.Albums, m)
		}
	}
	return b
}
