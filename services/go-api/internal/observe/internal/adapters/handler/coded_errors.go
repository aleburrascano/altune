package handler

import shareddomain "altune/go-api/internal/shared/domain"

var (
	errPrincipalRequired         = shareddomain.NewError(shareddomain.CodeObservePrincipalRequired, "overseer principal required")
	errDiscographyQualityTimeout = shareddomain.NewError(shareddomain.CodeObserveQualityTimeout, "discography quality query timed out")
	errEventFeedUnavailable      = shareddomain.NewError(shareddomain.CodeObserveEventFeedDown, "event feed unavailable")
	errStreamSubscriberLimit     = shareddomain.NewError(shareddomain.CodeObserveStreamLimit, "too many observe streams open")
	errStreamingUnsupported      = shareddomain.NewError(shareddomain.CodeObserveStreamingUnsupported, "streaming unsupported")
)

func streamUnavailable(stream string) error {
	return shareddomain.NewError(shareddomain.CodeObserveStreamUnavailable, stream+" stream unavailable")
}
