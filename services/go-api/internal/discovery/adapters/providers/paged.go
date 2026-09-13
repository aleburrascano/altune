package providers

// fetchPaged runs a paginated fetch loop under a shared degrade contract: a
// failure on page 0 is a hard error, while a failure on any later page returns
// the partial results gathered so far (after invoking onPartial for logging).
//
// Each caller supplies only its own per-page fetch+break logic and its own
// degrade log. fetch returns the page's items, whether more pages should be
// requested, and any fetch error. onPartial is called exactly once, on the
// later-page degrade path, with the failing page index and error.
func fetchPaged[T any](
	maxPages int,
	fetch func(page int) ([]T, bool, error),
	onPartial func(page int, err error),
) ([]T, error) {
	var all []T
	for page := 0; page < maxPages; page++ {
		items, more, err := fetch(page)
		if err != nil {
			if page > 0 {
				onPartial(page, err)
				return all, nil
			}
			return nil, err
		}
		all = append(all, items...)
		if !more {
			break
		}
	}
	return all, nil
}
