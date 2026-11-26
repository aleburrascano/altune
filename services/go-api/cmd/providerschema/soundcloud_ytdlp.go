package main

import (
	"altune/go-api/internal/shared/childprocess"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const soundCloudYtdlpTimeout = 90 * time.Second

type ytdlpRunner func(ctx context.Context, args []string) ([]byte, error)

func (soundCloud *soundCloudSession) ytdlpSearch(ctx context.Context, query string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, soundCloudYtdlpTimeout)
	defer cancel()
	out, err := soundCloud.ytdlp(ctx, []string{"--dump-json", "--flat-playlist", "--no-download", "scsearch5:" + query})
	if err != nil {
		return nil, fmt.Errorf("yt-dlp soundcloud search: %w", err)
	}
	first, _, _ := bytes.Cut(bytes.TrimSpace(out), []byte("\n"))
	if len(first) == 0 {
		return nil, fmt.Errorf("yt-dlp soundcloud search %q: no results", query)
	}
	return first, nil
}

func execYtdlp(ctx context.Context, args []string) ([]byte, error) {
	out, stderr, err := childprocess.Run(ctx, "yt-dlp", args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
	}
	return []byte(out), nil
}
