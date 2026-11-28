package service

import (
	"altune/go-api/internal/search/internal/ports"
	"context"
	"strings"
	"unicode/utf8"
)

const (
	MinSearchRunes = 2
	SearchLimit    = 20
)

func (s *UnreleasedService) Search(ctx context.Context, query string) ([]ports.UnreleasedHit, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < MinSearchRunes {
		return []ports.UnreleasedHit{}, nil
	}
	return s.backend.search(ctx, query)
}
