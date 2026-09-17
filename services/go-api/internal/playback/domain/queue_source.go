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

// normalized drops a source naming no subject. The source is a label on the
// queue, so losing it costs the user less than losing the save that carries the
// queue: rejecting it instead failed every save of a queue resumed with one,
// for as long as the client kept sending it back (#1577).
func (s QueueSource) normalized() QueueSource {
	if s.namesItsSubject() {
		return s
	}
	return QueueSource{}
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

// ParseQueueSource decodes a stored source_id. A token naming no subject
// ("playlist::", which rows written before #1569 still hold) parses to the zero
// source, so no reader can echo one back to a client as a playlist (#1577).
func ParseQueueSource(sourceId string) QueueSource {
	return decodeQueueSource(sourceId).normalized()
}

// decodeQueueSource returns the zero source only for a token it cannot decode,
// which is what lets a caller tell garbage from a token that decodes to a
// source naming no subject.
func decodeQueueSource(sourceId string) QueueSource {
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
	named := source.normalized()
	if named.IsZero() {
		return validatedFallback(fallback)
	}
	if !named.hasKnownKind() {
		return "", NewValidationError(fmt.Sprintf("unknown queue source kind: %q", named.Kind))
	}
	formatted := named.String()
	if formatted == "" {
		return validatedFallback(fallback)
	}
	return formatted, nil
}

// validatedFallback accepts a legacy source_id only when it decodes to a
// known-kind source, so garbage tokens are rejected instead of persisted
// (#620). A token that decodes but names no subject is neither: it is stored as
// no source, the answer its structured twin gets (#1577).
func validatedFallback(fallback string) (string, error) {
	decoded := decodeQueueSource(fallback)
	if fallback != "" && decoded.IsZero() {
		return "", NewValidationError(fmt.Sprintf("unrecognized legacy source_id: %q", fallback))
	}
	if !decoded.namesItsSubject() {
		return "", nil
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
