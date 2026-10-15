package main

import (
	acquisition "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/catalog/domain"
	"context"
	"net/url"
	"slices"
	"strings"
)

const (
	reasonNotInCatalog = "not-in-catalog"
	userUploadHost     = "soundcloud.com"
)

func (i identity) allAnswers() []sourceOutcome {
	return slices.Concat(i.Evidence.Sources, i.Evidence.ArtistFirst, i.Evidence.Search)
}

func (i identity) hasAnsweredCleanly() bool {
	return len(i.Evidence.Search) > 0 && !slices.ContainsFunc(i.allAnswers(), func(o sourceOutcome) bool { return o.Error != "" || o.Ambiguous })
}

func (i identity) listsTitle(title string) bool {
	return slices.ContainsFunc(i.allAnswers(), func(o sourceOutcome) bool {
		return slices.ContainsFunc(o.Candidates, func(j judged) bool { return titlesAgree(title, j.Title) })
	})
}

func isUnidentified(found identity) bool {
	return found.Status == statusUnbound && (found.Reason == reasonNoISRC || found.Reason == reasonISRCNotFound)
}

func isUserUploadPlatform(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == userUploadHost || strings.HasSuffix(host, "."+userUploadHost)
}

func markUncatalogued(_ context.Context, t libraryTrack, found identity) (identity, error) {
	title := matchingTrack(t, domain.CleanSearchKeys(t.Title, t.Artist, "")).Title
	if !isUnidentified(found) || !found.hasAnsweredCleanly() || found.listsTitle(title) {
		return found, nil
	}
	found.Status, found.Reason = statusUnbound, reasonNotInCatalog
	if t.SourceURL != "" {
		found.Evidence.SourceKey = acquisition.SourceKey(t.SourceURL)
	}
	if isUserUploadPlatform(t.SourceURL) {
		found.Status = statusUnofficial
	}
	return found, nil
}
