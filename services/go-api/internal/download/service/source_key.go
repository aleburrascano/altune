package service

import "altune/go-api/internal/download/sourcekey"

func sourceKey(rawURL string) string { return sourcekey.SourceKey(rawURL) }
