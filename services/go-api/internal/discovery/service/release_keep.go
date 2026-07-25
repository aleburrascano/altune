package service

func KeepRelease(m MergedRelease) bool {
	return m.IDVerified
}

func FilterKept(releases []MergedRelease) []MergedRelease {
	out := make([]MergedRelease, 0, len(releases))
	for _, m := range releases {
		if KeepRelease(m) {
			out = append(out, m)
		}
	}
	return out
}
