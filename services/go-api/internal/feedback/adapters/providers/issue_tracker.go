package providers

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 15 * time.Second
	maxErrorBody   = 4 << 10
	maxIssueBody   = 1 << 20
	sourceLabel    = "from-app"
	errPrefix      = "gitea issues"
	labelPageLimit = 100
)

func wrapErr(err error) error {
	return fmt.Errorf("%s: %w", errPrefix, err)
}

type GiteaIssueTracker struct {
	client  *http.Client
	baseURL string
	repo    string
	token   string

	labelsMu sync.Mutex
	labelIDs map[string]int64
}

func NewGiteaIssueTracker(baseURL, repo, token string) *GiteaIssueTracker {
	return &GiteaIssueTracker{
		client:  &http.Client{Timeout: requestTimeout},
		baseURL: strings.TrimSuffix(baseURL, "/"),
		repo:    repo,
		token:   token,
	}
}

var kindLabels = map[domain.Kind]string{
	domain.KindBug:       "bug",
	domain.KindIdea:      "enhancement",
	domain.KindConfusing: "ux",
}

func labelFor(kind domain.Kind) string {
	return kindLabels[kind]
}

type createIssueRequest struct {
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	Labels []int64 `json:"labels"`
}

type repoLabel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type createIssueResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func (t *GiteaIssueTracker) Create(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	req, err := t.newRequest(ctx, report, t.resolveLabels(ctx, report.Kind))
	if err != nil {
		return ports.IssueRef{}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return ports.IssueRef{}, transportError(err)
	}
	defer resp.Body.Close()
	defer drain(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return ports.IssueRef{}, statusError(resp, time.Now())
	}
	return t.readCreated(ctx, resp)
}

func (t *GiteaIssueTracker) readCreated(ctx context.Context, resp *http.Response) (ports.IssueRef, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIssueBody))
	if err != nil {
		return ports.IssueRef{}, outcomeUnknown(confirmedButUndecoded(ctx, resp.StatusCode, raw, wrapErr(fmt.Errorf("read issue: %w", err))))
	}
	ref, err := decodeIssue(raw)
	if err != nil {
		return ports.IssueRef{}, outcomeUnknown(confirmedButUndecoded(ctx, resp.StatusCode, raw, err))
	}
	return ref, nil
}

func confirmedButUndecoded(ctx context.Context, status int, raw []byte, err error) error {
	slog.ErrorContext(ctx, "gitea.issue_confirmed_but_undecoded",
		"status", status,
		"raw_body", boundedBody(raw),
		"error", err.Error(),
	)
	return err
}

func boundedBody(raw []byte) string {
	return strings.TrimSpace(string(raw[:min(len(raw), maxErrorBody)]))
}

func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxIssueBody))
}

func (t *GiteaIssueTracker) newRequest(ctx context.Context, report *domain.Report, labels []int64) (*http.Request, error) {
	payload, err := json.Marshal(createIssueRequest{
		Title:  plainTitle(report.Title()),
		Body:   renderBody(report, logging.CorrelationIDFromContext(ctx)),
		Labels: labels,
	})
	if err != nil {
		return nil, fmt.Errorf("encode issue: %w", err)
	}
	url := fmt.Sprintf("%s/api/v1/repos/%s/issues", t.baseURL, t.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build issue request: %w", err)
	}
	setHeaders(req, t.token)
	return req, nil
}

func setHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
}

func readErrorBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return strings.TrimSpace(string(body))
}

func decodeIssue(raw []byte) (ports.IssueRef, error) {
	var created createIssueResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		return ports.IssueRef{}, wrapErr(fmt.Errorf("decode issue: %w", err))
	}
	if created.Number == 0 {
		return ports.IssueRef{}, wrapErr(errors.New("response carried no issue number"))
	}
	return ports.IssueRef{Number: created.Number, URL: created.HTMLURL}, nil
}

func (t *GiteaIssueTracker) resolveLabels(ctx context.Context, kind domain.Kind) []int64 {
	known, err := t.repoLabels(ctx)
	if err != nil {
		slog.WarnContext(ctx, "gitea.label_lookup_failed", "error", err.Error())
		return []int64{}
	}
	ids := []int64{}
	for _, name := range []string{labelFor(kind), sourceLabel} {
		if name == "" {
			continue
		}
		id, ok := known[name]
		if !ok {
			slog.WarnContext(ctx, "gitea.label_missing", "label", name)
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (t *GiteaIssueTracker) repoLabels(ctx context.Context) (map[string]int64, error) {
	t.labelsMu.Lock()
	defer t.labelsMu.Unlock()
	if t.labelIDs != nil {
		return t.labelIDs, nil
	}
	labels, err := t.fetchLabels(ctx)
	if err != nil {
		return nil, err
	}
	t.labelIDs = labels
	return labels, nil
}

func (t *GiteaIssueTracker) fetchLabels(ctx context.Context) (map[string]int64, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/labels?limit=%d", t.baseURL, t.repo, labelPageLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, wrapErr(fmt.Errorf("build label request: %w", err))
	}
	setHeaders(req, t.token)
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, wrapErr(fmt.Errorf("list labels: %w", err))
	}
	defer resp.Body.Close()
	defer drain(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, wrapErr(fmt.Errorf("list labels: status %d", resp.StatusCode))
	}
	var listed []repoLabel
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxIssueBody)).Decode(&listed); err != nil {
		return nil, wrapErr(fmt.Errorf("decode labels: %w", err))
	}
	byName := make(map[string]int64, len(listed))
	for _, label := range listed {
		byName[label.Name] = label.ID
	}
	return byName, nil
}
