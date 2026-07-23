package service

// Confidence-keep — build step 2 of the discography rebuild (doc §6). Replaces
// the MusicBrainz-authority VETO (fault F4: MB was both the contamination filter
// AND a purger of real releases MB hadn't catalogued). Here MB is just one vote;
// a release is kept on POSITIVE evidence that it is genuinely the artist's, so an
// incomplete MB discography can never drop a real release (e.g. REST IN BASS:
// ENCORE), while an uncorroborated same-name namesake still gets dropped. Pure.

// KeepRelease reports whether a merged release belongs in the discography. It is
// kept when ANY of these hold:
//   - IDVerified: at least one provider returned it when queried by the artist's
//     OWN id — definitionally the artist's, so it cannot be a namesake.
//   - HasStrongID: it carries a UPC/MBID/ISRC — an identifier, not a name guess.
//   - corroborated: two or more distinct providers independently list it.
//
// Only the residue is dropped: a single provider, reached by NAME, with no
// identifier — the exact shape of a same-name artist's release leaking in through
// a by-name completeness fetch.
func KeepRelease(m MergedRelease) bool {
	if m.IDVerified || m.HasStrongID {
		return true
	}
	return len(m.Providers) >= 2
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
