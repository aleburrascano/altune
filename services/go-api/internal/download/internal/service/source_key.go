package service

import "altune/go-api/internal/download/internal/domain"

func sourceKey(rawURL string) string { return domain.SourceKey(rawURL) }
