package authtest

import (
	authtelemetry "altune/go-api/internal/platform/auth/internal/adapters/telemetry"
	"altune/go-api/internal/platform/auth/internal/domain"
)

type MetricsSnapshot = authtelemetry.Snapshot

const ReasonSignatureInvalid = domain.ReasonSignatureInvalid
