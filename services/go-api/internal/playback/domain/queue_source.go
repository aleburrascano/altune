package domain

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	SourceKindLibrary  = "library"
	SourceKindPlaylist = "playlist"
	SourceKindSearch   = "search"
)

type QueueSource struct {
	Kind       string
	PlaylistId string
	Name       string
	Query      string
}

func (s QueueSource) IsZero() bool {
	return s.Kind == ""
}

func (s QueueSource) hasKnownKind() bool {
	switch s.Kind {
	case SourceKindLibrary, SourceKindPlaylist, SourceKindSearch:
		return true
	}
	return false
}

// namesItsSubject is false for a playlist source carrying no playlist id: it
// formats to "playlist::", a non-empty token stored as if it referenced a real
// playlist, rather than the zero source the caller actually described (#1569).
func (s QueueSource) namesItsSubject() bool {
	return s.Kind != SourceKindPlaylist || s.PlaylistId != ""
}

func (s QueueSource) String() string {
	switch s.Kind {
	case SourceKindPlaylist:
		return SourceKindPlaylist + ":" + url.QueryEscape(s.PlaylistId) + ":" + url.QueryEscape(s.Name)
	case SourceKindSearch:
		if s.Query == "" {
			return SourceKindSearch
		}
		return SourceKindSearch + ":" + url.QueryEscape(s.Query)
	case SourceKindLibrary:
		return SourceKindLibrary
	}
	return ""
}

func ParseQueueSource(sourceId string) QueueSource {
	if sourceId == "" {
		return QueueSource{}
	}
	if rest, found := strings.CutPrefix(sourceId, SourceKindPlaylist+":"); found {
		rawId, rawName, hasName := strings.Cut(rest, ":")
		playlistId, ok := unescape(rawId)
		if !ok {
			return QueueSource{}
		}
		source := QueueSource{Kind: SourceKindPlaylist, PlaylistId: playlistId}
		if hasName {
			name, ok := unescape(rawName)
			if !ok {
				return QueueSource{}
			}
			source.Name = name
		}
		return source
	}
	if rest, found := strings.CutPrefix(sourceId, SourceKindSearch+":"); found {
		query, ok := unescape(rest)
		if !ok {
			return QueueSource{}
		}
		return QueueSource{Kind: SourceKindSearch, Query: query}
	}
	switch sourceId {
	case SourceKindLibrary:
		return QueueSource{Kind: SourceKindLibrary}
	case SourceKindSearch:
		return QueueSource{Kind: SourceKindSearch}
	}
	return QueueSource{}
}

func FormatQueueSource(source QueueSource, fallback string) (string, error) {
	if source.IsZero() {
		return validatedFallback(fallback)
	}
	if !source.hasKnownKind() {
		return "", NewValidationError(fmt.Sprintf("unknown queue source kind: %q", source.Kind))
	}
	if !source.namesItsSubject() {
		return "", NewValidationError("queue source kind \"playlist\" requires a playlist_id")
	}
	formatted := source.String()
	if formatted == "" {
		return validatedFallback(fallback)
	}
	return formatted, nil
}

// validatedFallback accepts a legacy source_id only when it decodes to a
// known-kind source, so garbage tokens are rejected instead of persisted.
func validatedFallback(fallback string) (string, error) {
	if fallback == "" {
		return "", nil
	}
	if ParseQueueSource(fallback).IsZero() {
		return "", NewValidationError(fmt.Sprintf("unrecognized legacy source_id: %q", fallback))
	}
	return fallback, nil
}

func unescape(value string) (string, bool) {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return "", false
	}
	return decoded, true
}
