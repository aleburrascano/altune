package main

import (
	"context"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"sync"
)

const maxCoverBytes = 10 << 20

type coverHash struct {
	hash uint64
	err  error
}

type coverComparer struct {
	http  *http.Client
	mu    sync.Mutex
	cache map[string]coverHash
}

func newCoverComparer(client *http.Client) *coverComparer {
	return &coverComparer{http: client, cache: map[string]coverHash{}}
}

func (c *coverComparer) SameImage(ctx context.Context, libraryURL, catalogURL string) (bool, error) {
	a, err := c.hashOf(ctx, libraryURL)
	if err != nil {
		return false, err
	}
	b, err := c.hashOf(ctx, catalogURL)
	if err != nil {
		return false, err
	}
	return isSameImage(a, b), nil
}

func (c *coverComparer) hashOf(ctx context.Context, url string) (uint64, error) {
	c.mu.Lock()
	cached, ok := c.cache[url]
	c.mu.Unlock()
	if ok {
		return cached.hash, cached.err
	}
	hash, err := c.fetchHash(ctx, url)
	c.mu.Lock()
	c.cache[url] = coverHash{hash: hash, err: err}
	c.mu.Unlock()
	return hash, err
}

func (c *coverComparer) fetchHash(ctx context.Context, url string) (uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("build cover request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch cover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fetch cover: status %d", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxCoverBytes))
	if err != nil {
		return 0, fmt.Errorf("decode cover: %w", err)
	}
	return averageHash(img), nil
}

func coverURLContainsImage(artworkURL, imageMD5 string) bool {
	return imageMD5 != "" && strings.Contains(artworkURL, imageMD5)
}
