package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const oembedBodyCap = 1 << 20

var errNoOEmbedResponse = errors.New("soundcloud oembed: no response")

type youtubeClient interface {
	Watch(ctx context.Context, videoID string) (ytmsearch.Watch, error)
	OEmbed(ctx context.Context, videoID string) (ytmsearch.OEmbed, error)
}

type sourceLookup struct {
	youtube youtubeClient
	http    *http.Client
	pace    *pacer
}

func newSourceLookup(youtube youtubeClient, minInterval time.Duration) *sourceLookup {
	return &sourceLookup{
		youtube: youtube,
		http:    &http.Client{Timeout: lookupTimeout},
		pace:    &pacer{interval: minInterval},
	}
}

func (l *sourceLookup) Watch(ctx context.Context, videoID string) (ytmsearch.Watch, error) {
	return l.youtube.Watch(ctx, videoID)
}

func (l *sourceLookup) YouTubeAuthor(ctx context.Context, videoID string) (string, error) {
	oembed, err := l.youtube.OEmbed(ctx, videoID)
	return oembed.Author, err
}

func (l *sourceLookup) SoundCloudAuthor(ctx context.Context, trackURL string) (string, error) {
	if err := l.pace.wait(ctx); err != nil {
		return "", err
	}
	raw, err := l.fetchSoundCloudOEmbed(ctx, trackURL)
	if err != nil {
		return "", err
	}
	oembed, err := ytmsearch.ParseOEmbed(raw)
	return oembed.Author, err
}

func (l *sourceLookup) fetchSoundCloudOEmbed(ctx context.Context, trackURL string) ([]byte, error) {
	query := url.Values{"url": {trackURL}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, soundCloudOEmbedURL+"?"+query.Encode(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build soundcloud oembed request: %w", err)
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("soundcloud oembed: %w", err)
	}
	if resp == nil {
		return nil, errNoOEmbedResponse
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("soundcloud oembed: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, oembedBodyCap))
}

type pacer struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	start := p.next
	if start.Before(now) {
		start = now
	}
	p.next = start.Add(p.interval)
	p.mu.Unlock()
	select {
	case <-time.After(time.Until(start)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
