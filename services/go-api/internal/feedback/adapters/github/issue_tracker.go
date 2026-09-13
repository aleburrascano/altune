package github

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
)

type IssueTracker struct {
	client  *http.Client
	baseURL string
	repo    string
	token   string
}

func NewIssueTracker(repo, token string) *IssueTracker {
	return &IssueTracker{
		client:  &http.Client{Timeout: requestTimeout},
		baseURL: defaultBaseURL,
		repo:    repo,
		token:   token,
	}
}

func (t *IssueTracker) WithBaseURL(baseURL string) *IssueTracker {
	t.baseURL = strings.TrimSuffix(baseURL, "/")
	return t
}

// kindLabels maps a feedback Kind to the GitHub issue label its issue gets.
// The label vocabulary is GitHub's, so it lives here in the adapter rather than
// in the domain. An undefined kind maps to "", never mislabelling it as a bug.
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

func (t *IssueTracker) Create(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	req, err := t.newRequest(ctx, report)
	if err != nil {
		return ports.IssueRef{}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return ports.IssueRef{}, fmt.Errorf("github issues: %w", err)
	}
	defer resp.Body.Close()
	defer drain(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return ports.IssueRef{}, statusError(resp)
	}
	return decodeIssue(io.LimitReader(resp.Body, maxIssueBody))
}

// drain discards what is left of a body, bounded, before it is closed so the
// keep-alive connection can be reused instead of torn down.
func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxIssueBody))
}

func (t *IssueTracker) newRequest(ctx context.Context, report *domain.Report) (*http.Request, error) {
	payload, err := json.Marshal(createIssueRequest{
		Title:  report.Title(),
		Body:   renderBody(report),
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

func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return fmt.Errorf("github issues: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func decodeIssue(body io.Reader) (ports.IssueRef, error) {
	var created createIssueResponse
	if err := json.NewDecoder(body).Decode(&created); err != nil {
		return ports.IssueRef{}, fmt.Errorf("decode issue: %w", err)
	}
	if created.Number == 0 {
		return ports.IssueRef{}, fmt.Errorf("github issues: response carried no issue number")
	}
	return ports.IssueRef{Number: created.Number, URL: created.HTMLURL}, nil
}
