package providers

import "altune/go-api/internal/discovery/domain"

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
				return all, &domain.PartialResultError{Page: page, Err: err}
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
