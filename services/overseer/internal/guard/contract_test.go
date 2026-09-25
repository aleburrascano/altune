package guard_test

import (
	"altune/overseer/internal/authn"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/shell"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

var updateFixtures = flag.Bool("update", false, "rewrite the contract fixtures from the current Go wire JSON")

const (
	contractOwnerID = "contract-owner"
	contractToken   = "contract-owner-token"
)

var contractTime = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type contractVerifier struct{}

func (contractVerifier) Verify(_ context.Context, token string) (authn.Claims, error) {
	if token != contractToken {
		return authn.Claims{}, errors.New("invalid token")
	}
	return authn.Claims{Subject: contractOwnerID}, nil
}

type contractBucket struct{}

func (contractBucket) Meta() core.Meta                                { return core.Meta{ID: "reliability", Title: "Reliability"} }
func (contractBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (contractBucket) Store([]core.Signal)                            {}
func (contractBucket) KeySeries() string                              { return "latency_ms" }

func (contractBucket) Snapshot() core.Snapshot {
	return core.Snapshot{
		ID:        "reliability",
		Title:     "Reliability",
		State:     core.StateSourceDown,
		Severity:  core.SeverityWarn,
		Headline:  "p95 latency 240ms",
		Reason:    goapi.ReasonDegraded,
		UpdatedAt: contractTime,
		Data:      json.RawMessage(`{"p50":80,"p95":240,"p99":410}`),
	}
}

type contractRegistry struct{ bucket core.Bucket }

func (r contractRegistry) Buckets() []core.Bucket { return []core.Bucket{r.bucket} }

func (r contractRegistry) Get(id string) (core.Bucket, bool) {
	if id == r.bucket.Meta().ID {
		return r.bucket, true
	}
	return nil, false
}

type contractSeries struct{}

func (contractSeries) Names(string) ([]string, error) { return []string{"latency_ms"}, nil }

func (contractSeries) Query(_, _ string, _, _ time.Time) ([]core.Point, error) {
	return []core.Point{{At: contractTime, Value: 240}}, nil
}

func (contractSeries) Minutes(_, _ string, _, _ time.Time) ([]core.Minute, error) {
	return []core.Minute{{At: contractTime, Min: 80, Max: 410, Avg: 240}}, nil
}

func contractServer() http.Handler {
	return shell.NewHandler(
		contractRegistry{bucket: contractBucket{}},
		shell.WithVerifier(contractVerifier{}),
		shell.WithOwnerUserID(contractOwnerID),
		shell.WithSeries(contractSeries{}),
		shell.WithCollectStatus(func() shell.CollectStatus {
			return shell.CollectStatus{Healthy: true, LastCycle: contractTime, OK: 8, Failed: 1}
		}),
		shell.WithCredentialHealth(func() goapi.CredentialHealth {
			return goapi.CredentialHealth{
				LastRefresh:         contractTime,
				ConsecutiveFailures: 0,
				PersistFailed:       false,
				PasswordGrant:       true,
				LastError:           "invalid_grant",
			}
		}),
	).Router()
}

func contractGet(t *testing.T, srv http.Handler, path string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+contractToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200 (%s)", path, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

const fixtureDir = "../../web/src/test/fixtures"

func writeFixture(t *testing.T, path string, pretty []byte) {
	t.Helper()
	if err := os.WriteFile(path, pretty, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func assertFixtureMatches(t *testing.T, path string, pretty []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v (generate it with: go test ./internal/guard/... -run TestContract -update)", path, err)
	}
	if !bytes.Equal(pretty, want) {
		t.Errorf("%s has drifted from the Go wire JSON.\n got:\n%s\nwant:\n%s\n(if this is intentional, run: go test ./internal/guard/... -run TestContract -update, then update web/src/types.ts to match)",
			path, pretty, want)
	}
}

func pinFixture(t *testing.T, name string, wire []byte) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, wire, "", "  "); err != nil {
		t.Fatalf("wire JSON for %s does not parse: %v (%s)", name, err, wire)
	}
	pretty.WriteByte('\n')
	path := fixtureDir + "/" + name

	if *updateFixtures {
		writeFixture(t, path, pretty.Bytes())
		return
	}
	assertFixtureMatches(t, path, pretty.Bytes())
}

func TestContractSnapshotFixtureMatchesTheBucketsWireJSON(t *testing.T) {
	srv := contractServer()
	body := contractGet(t, srv, "/api/buckets")

	var envelope struct {
		Buckets []json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode /api/buckets: %v (%s)", err, body)
	}
	if len(envelope.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(envelope.Buckets))
	}
	pinFixture(t, "snapshot.json", envelope.Buckets[0])
}

func TestContractSeriesFixtureMatchesTheSeriesWireJSON(t *testing.T) {
	srv := contractServer()
	body := contractGet(t, srv, "/api/buckets/reliability/series?range=7d")
	pinFixture(t, "series.json", body)
}

func TestContractHealthFixtureMatchesTheHealthWireJSON(t *testing.T) {
	srv := contractServer()
	body := contractGet(t, srv, "/api/health")
	pinFixture(t, "health.json", body)
}
