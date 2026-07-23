package service

// Confidence-keep — build step 2 of the discography rebuild (doc §6). Replaces
// the MusicBrainz-authority VETO (fault F4: MB was both the contamination filter
// AND a purger of real releases MB hadn't catalogued). Here MB is just one vote;
// a release is kept on POSITIVE evidence that it is genuinely the artist's, so an
// incomplete MB discography can never drop a real release (e.g. REST IN BASS:
// ENCORE), while an uncorroborated same-name namesake still gets dropped. Pure.

// KeepRelease reports whether a merged release belongs in the discography. The
// only sound signal is IDVerified: at least one provider returned this release
// when queried by the artist's VERIFIED id (own provider id or MBID) — so it is
// definitionally this artist's, never a namesake.
//
// A strong identifier (HasStrongID) and by-name corroboration were tried and are
// UNSOUND for ambiguous names (doc §6 correction): a same-name artist's release
// carries its own valid MBID, and two by-name providers can return the same wrong
// artist. No per-release signal separates the right artist from a namesake — only
// the provenance of the query does. By-name groups still MERGE (they enrich an
// id-verified cluster's metadata); a cluster with no id-verified source is dropped.
func KeepRelease(m MergedRelease) bool {
	return m.IDVerified
}

// FilterKept returns only the releases KeepRelease accepts, preserving order.
func FilterKept(releases []MergedRelease) []MergedRelease {
	out := make([]MergedRelease, 0, len(releases))
	for _, m := range releases {
		if KeepRelease(m) {
			out = append(out, m)
		}
	}
	return out
}
