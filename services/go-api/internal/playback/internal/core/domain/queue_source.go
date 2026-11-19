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

func (s QueueSource) namesItsSubject() bool {
	return s.Kind != SourceKindPlaylist || s.PlaylistId != ""
}

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

func ParseQueueSource(sourceId string) QueueSource {
	return decodeQueueSource(sourceId).normalized()
}

func decodeQueueSource(sourceId string) QueueSource {
	if rest, found := strings.CutPrefix(sourceId, SourceKindPlaylist+":"); found {
		return decodePlaylistSource(rest)
	}
	if rest, found := strings.CutPrefix(sourceId, SourceKindSearch+":"); found {
		return decodeSearchSource(rest)
	}
	return decodeKindOnlySource(sourceId)
}

func decodePlaylistSource(rest string) QueueSource {
	rawId, rawName, _ := strings.Cut(rest, ":")
	playlistId, idDecoded := unescape(rawId)
	name, nameDecoded := unescape(rawName)
	if !idDecoded || !nameDecoded {
		return QueueSource{}
	}
	return QueueSource{Kind: SourceKindPlaylist, PlaylistId: playlistId, Name: name}
}

func decodeSearchSource(rest string) QueueSource {
	query, decoded := unescape(rest)
	if !decoded {
		return QueueSource{}
	}
	return QueueSource{Kind: SourceKindSearch, Query: query}
}

func decodeKindOnlySource(sourceId string) QueueSource {
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
		return "", newValidationError(codeUnknownSourceKind, fmt.Sprintf("unknown queue source kind: %q", named.Kind))
	}
	return named.String(), nil
}

func validatedFallback(fallback string) (string, error) {
	decoded := decodeQueueSource(fallback)
	if fallback != "" && decoded.IsZero() {
		return "", newValidationError(codeUnrecognizedSourceId, fmt.Sprintf("unrecognized legacy source_id: %q", fallback))
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
