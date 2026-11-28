package handler

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/apierror"
	"errors"
	"time"
)

type retryAfterer interface {
	RetryAfter() time.Duration
}

var wireErrors = []struct {
	domain error
	wire   *apierror.CodedError
}{
	{domain.ErrRetryNotFailed, apierror.NewError(apierror.CodeAcquisitionRetryNotFailed, domain.ErrRetryNotFailed.Error())},
	{domain.ErrCooldownActive, apierror.NewError(apierror.CodeAcquisitionCooldownActive, domain.ErrCooldownActive.Error())},
	{domain.ErrReacquireNotReady, apierror.NewError(apierror.CodeAcquisitionReacquireNotReady, domain.ErrReacquireNotReady.Error())},
	{domain.ErrTrackJobInFlight, apierror.NewError(apierror.CodeAcquisitionJobInFlight, domain.ErrTrackJobInFlight.Error())},
	{domain.ErrSchedulerShutdown, apierror.NewError(apierror.CodeAcquisitionShuttingDown, domain.ErrSchedulerShutdown.Error())},
	{domain.ErrAcquisitionPaused, apierror.NewError(apierror.CodeAcquisitionPaused, domain.ErrAcquisitionPaused.Error())},
	{domain.ErrTrackNotFound, apierror.NewError(apierror.CodeCatalogTrackNotFound, "track not found")},
}

var errInvalidTrackID = apierror.NewError(apierror.CodeCatalogInvalidTrackID, "invalid track ID")

func MapError(err error) error {
	for _, row := range wireErrors {
		if !errors.Is(err, row.domain) {
			continue
		}
		var retryable retryAfterer
		if errors.As(err, &retryable) {
			return row.wire.WithRetryAfter(retryable.RetryAfter())
		}
		return row.wire
	}
	return err
}
