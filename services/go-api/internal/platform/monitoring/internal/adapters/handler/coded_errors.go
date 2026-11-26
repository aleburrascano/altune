package handler

import apierror "altune/go-api/internal/shared/apierror"

var (
	errPrincipalRequired         = apierror.NewError(apierror.CodeObservePrincipalRequired, "overseer principal required")
	errDiscographyQualityTimeout = apierror.NewError(apierror.CodeObserveQualityTimeout, "discography quality query timed out")
	errEventFeedUnavailable      = apierror.NewError(apierror.CodeObserveEventFeedDown, "event feed unavailable")
	errStreamSubscriberLimit     = apierror.NewError(apierror.CodeObserveStreamLimit, "too many observe streams open")
	errStreamingUnsupported      = apierror.NewError(apierror.CodeObserveStreamingUnsupported, "streaming unsupported")
)

func invalidGroupBy(cause error) error {
	return apierror.NewError(apierror.CodeObserveInvalidGroupBy, cause.Error())
}

func streamUnavailable(stream string) error {
	return apierror.NewError(apierror.CodeObserveStreamUnavailable, stream+" stream unavailable")
}
