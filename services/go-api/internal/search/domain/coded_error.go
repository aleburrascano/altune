package domain

import (
	apierror "altune/go-api/internal/shared/apierror"
	"fmt"
)

var ErrRateLimited = apierror.NewError(apierror.CodeDiscoveryRateLimited, "too many requests, try again later")

func NewInvalidFavoriteError(msg string) *apierror.CodedError {
	return apierror.NewError(apierror.CodeDiscoveryInvalidFavorite, msg)
}

func NewFavoritesFullError(limit int) *apierror.CodedError {
	return apierror.NewError(apierror.CodeDiscoveryFavoritesFull, fmt.Sprintf("favorites are full: at most %d per account", limit))
}

func NewInvalidEventError(msg string) *apierror.CodedError {
	return apierror.NewError(apierror.CodeDiscoveryInvalidEvent, msg)
}
