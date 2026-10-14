package main

import (
	"context"
	"fmt"
	"slices"
)

type blankJudge interface {
	IsBlank(ctx context.Context, url string) (bool, error)
}

type albumKindCatalog interface {
	AlbumRecordType(ctx context.Context, albumID int64) (string, error)
}

var ownAlbumRecordTypes = []string{"album", "ep"}

func coverFixFindings(ctx context.Context, cat catalog, covers coverJudge, lib libraryTrack, found catalogTrack, findings []finding) ([]finding, error) {
	judge, canJudgeBlank := covers.(blankJudge)
	if canJudgeBlank && lib.ArtworkURL != "" {
		if blank, err := judge.IsBlank(ctx, lib.ArtworkURL); err == nil && blank {
			findings = append(findings, about(lib, categoryCoverBlank, lib.ArtworkURL))
		}
	}
	index := slices.IndexFunc(findings, func(f finding) bool { return f.Category == categoryCoverImageDiffers })
	if index < 0 {
		return findings, nil
	}
	replacement, reason, err := replacementCover(ctx, cat, judge, lib, found)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return append(findings, about(lib, categoryCoverNotReplaceable, reason)), nil
	}
	findings[index].Fix = fixing(lib, "artwork_url", lib.ArtworkURL, replacement)
	return findings, nil
}

func replacementCover(ctx context.Context, cat catalog, judge blankJudge, lib libraryTrack, found catalogTrack) (string, string, error) {
	kinds, knowsKinds := cat.(albumKindCatalog)
	if reason := notOwnAlbumReason(lib, found, knowsKinds); reason != "" {
		return "", reason, nil
	}
	recordType, err := kinds.AlbumRecordType(ctx, found.AlbumID)
	if err != nil {
		return "", "album type lookup failed: " + err.Error(), blockingOnly(err)
	}
	if !slices.Contains(ownAlbumRecordTypes, recordType) {
		return "", fmt.Sprintf("catalog album is a %q, not an album or ep", recordType), nil
	}
	if isBlankOrUnreadable(ctx, judge, found.CoverURL) {
		return "", "catalog cover is blank or unreadable", nil
	}
	return found.FullCoverURL, "", nil
}

func notOwnAlbumReason(lib libraryTrack, found catalogTrack, knowsKinds bool) string {
	switch {
	case !sharesLibraryAlbum(lib, found):
		return fmt.Sprintf("catalog album %q is not the library album %q", found.Album, lib.Album)
	case found.AlbumID == 0 || found.FullCoverURL == "":
		return "catalog track has no album id or cover"
	case !knowsKinds:
		return "album type was not consulted"
	}
	return ""
}

func isBlankOrUnreadable(ctx context.Context, judge blankJudge, url string) bool {
	if judge == nil {
		return false
	}
	blank, err := judge.IsBlank(ctx, url)
	return err != nil || blank
}
