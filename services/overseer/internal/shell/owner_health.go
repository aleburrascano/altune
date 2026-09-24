package shell

import (
	"altune/overseer/internal/goapi"
	"net/http"
	"time"
)

type ownerHealthResponse struct {
	LastCycle     string              `json:"lastCycle,omitempty"`
	BucketsOK     int                 `json:"bucketsOk"`
	BucketsFailed int                 `json:"bucketsFailed"`
	Credential    *credentialResponse `json:"credential,omitempty"`
}

type credentialResponse struct {
	OK                  bool   `json:"ok"`
	LastRefresh         string `json:"lastRefresh,omitempty"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	PersistFailed       bool   `json:"persistFailed"`
	PasswordGrant       bool   `json:"passwordGrant"`
	LastError           string `json:"lastError,omitempty"`
}

func WithCredentialHealth(health func() goapi.CredentialHealth) Option {
	return func(h *Handler) {
		if health != nil {
			h.credentialHealth = health
		}
	}
}

func (h *Handler) handleOwnerHealth(w http.ResponseWriter, _ *http.Request) {
	status := h.collectStatus()
	writeJSON(w, http.StatusOK, ownerHealthResponse{
		LastCycle:     rfc3339OrEmpty(status.LastCycle),
		BucketsOK:     status.OK,
		BucketsFailed: status.Failed,
		Credential:    h.credential(),
	})
}

func (h *Handler) credential() *credentialResponse {
	if h.credentialHealth == nil {
		return nil
	}
	health := h.credentialHealth()
	return &credentialResponse{
		OK:                  health.OK(),
		LastRefresh:         rfc3339OrEmpty(health.LastRefresh),
		ConsecutiveFailures: health.ConsecutiveFailures,
		PersistFailed:       health.PersistFailed,
		PasswordGrant:       health.PasswordGrant,
		LastError:           health.LastError,
	}
}

func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
