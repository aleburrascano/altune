package unreleased

import (
	"altune/go-api/internal/discovery/ports"
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
	if s.store == nil || utf8.RuneCountInString(query) < MinSearchRunes {
		return []ports.UnreleasedHit{}, nil
	}
	return s.store.Search(ctx, query, SearchLimit)
}
