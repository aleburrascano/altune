package domain

import (
	shareddomain "altune/go-api/internal/shared/domain"
	"fmt"
)

var ErrRateLimited = shareddomain.NewError(shareddomain.CodeDiscoveryRateLimited, "too many requests, try again later")

func NewInvalidFavoriteError(msg string) *shareddomain.CodedError {
	return shareddomain.NewError(shareddomain.CodeDiscoveryInvalidFavorite, msg)
}

func NewFavoritesFullError(limit int) *shareddomain.CodedError {
	return shareddomain.NewError(shareddomain.CodeDiscoveryFavoritesFull, fmt.Sprintf("favorites are full: at most %d per account", limit))
}

func NewInvalidEventError(msg string) *shareddomain.CodedError {
	return shareddomain.NewError(shareddomain.CodeDiscoveryInvalidEvent, msg)
}
