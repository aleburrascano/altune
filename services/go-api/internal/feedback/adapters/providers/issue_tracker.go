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
	"time"
)

const (
	defaultBaseURL = "https://api.github.com"
	apiVersion     = "2022-11-28"
	requestTimeout = 15 * time.Second
	maxErrorBody   = 4 << 10
	maxIssueBody   = 1 << 20
	sourceLabel    = "from-app"
	errPrefix      = "github issues"
)

func wrapErr(err error) error {
	return fmt.Errorf("%s: %w", errPrefix, err)
}

type GitHubIssueTracker struct {
	client  *http.Client
	baseURL string
	repo    string
	token   string
}

func NewGitHubIssueTracker(repo, token string) *GitHubIssueTracker {
	return &GitHubIssueTracker{
		client:  &http.Client{Timeout: requestTimeout},
		baseURL: defaultBaseURL,
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
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

type createIssueResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func (t *GitHubIssueTracker) Create(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	req, err := t.newRequest(ctx, report)
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

func (t *GitHubIssueTracker) readCreated(ctx context.Context, resp *http.Response) (ports.IssueRef, error) {
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
	slog.ErrorContext(ctx, "github.issue_confirmed_but_undecoded",
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

func (t *GitHubIssueTracker) newRequest(ctx context.Context, report *domain.Report) (*http.Request, error) {
	payload, err := json.Marshal(createIssueRequest{
		Title:  plainTitle(report.Title()),
		Body:   renderBody(report, logging.CorrelationIDFromContext(ctx)),
		Labels: []string{labelFor(report.Kind), sourceLabel},
	})
	if err != nil {
		return nil, fmt.Errorf("encode issue: %w", err)
	}
	url := fmt.Sprintf("%s/repos/%s/issues", t.baseURL, t.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build issue request: %w", err)
	}
	setHeaders(req, t.token)
	return req, nil
}

func setHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
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
