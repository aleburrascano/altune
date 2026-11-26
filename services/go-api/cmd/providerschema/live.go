package main

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/providerfields"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const responseCap = 32 << 20

type fetcher func(ctx context.Context, id string) (json.RawMessage, error)

type liveSource map[string]fetcher

func (liveSource) Reference(context.Context, string) (providerfields.Observed, error) {
	return providerfields.Observed{}, nil
}

func (live liveSource) Resource(ctx context.Context, entity, id string) (json.RawMessage, error) {
	fetch, ok := live[entity]
	if !ok {
		return nil, fmt.Errorf("no fetcher for entity %q", entity)
	}
	return fetch(ctx, id)
}

type decodedAs string

const decodedByKeys decodedAs = ""

func (typeName decodedAs) goType() string { return string(typeName) }

type endpoint interface{ goType() string }

func decodedTypes[E endpoint](endpoints map[string]E) map[string]string {
	types := make(map[string]string, len(endpoints))
	for entity, entry := range endpoints {
		types[entity] = entry.goType()
	}
	return types
}

func fixtureParts(id string, names ...string) ([]string, error) {
	parts := strings.Split(id, "|")
	if len(parts) != len(names) {
		return nil, fmt.Errorf("fixture %q is not %s", id, strings.Join(names, "|"))
	}
	return parts, nil
}

func fixtureNumbers(id string, names ...string) ([]int, error) {
	parts, err := fixtureParts(id, names...)
	if err != nil {
		return nil, err
	}
	numbers := make([]int, len(parts))
	for i, name := range names {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("fixture %q: %s is not a positive number", id, name)
		}
		numbers[i] = n
	}
	return numbers, nil
}

type request struct {
	method string
	url    string
	body   []byte
	header map[string]string
	secret string
}

func fetch(ctx context.Context, client *http.Client, r request) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, r.method, r.url, bytes.NewReader(r.body))
	if err != nil {
		return nil, err
	}
	for name, value := range r.header {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", r.label(req.URL), withoutURL(err))
	}
	if resp == nil {
		return nil, fmt.Errorf("%s: no response", r.label(req.URL))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError{request: r.label(req.URL), status: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, responseCap))
}

func pacedFetch(ctx context.Context, client *http.Client, gate *intervalGate, r request) ([]byte, error) {
	if err := gate.wait(ctx); err != nil {
		return nil, err
	}
	return fetch(ctx, client, r)
}

type statusError struct {
	request string
	status  int
}

func (failure statusError) Error() string {
	return fmt.Sprintf("%s: status %d", failure.request, failure.status)
}

func (r request) label(target *url.URL) string {
	label := r.method + " " + target.Host + target.Path
	if r.secret == "" {
		return label
	}
	return strings.NewReplacer(r.secret, "***", url.PathEscape(r.secret), "***").Replace(label)
}

func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

type intervalGate struct {
	mu   sync.Mutex
	gap  time.Duration
	next time.Time
}

func newIntervalGate(gap time.Duration) *intervalGate {
	return &intervalGate{gap: gap}
}

func (gate *intervalGate) wait(ctx context.Context) error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if err := clock.Sleep(ctx, time.Until(gate.next)); err != nil {
		return err
	}
	gate.next = time.Now().Add(gate.gap)
	return nil
}
