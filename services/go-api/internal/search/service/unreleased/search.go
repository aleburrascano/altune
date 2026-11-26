package unreleased

import (
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"strings"
	"unicode/utf8"
)

const (
	MinSearchRunes = 2
	SearchLimit    = 20
)

func (s *Service) Search(ctx context.Context, query string) ([]ports.UnreleasedHit, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < MinSearchRunes {
		return []ports.UnreleasedHit{}, nil
	}
	return s.storage.search(ctx, query)
}
