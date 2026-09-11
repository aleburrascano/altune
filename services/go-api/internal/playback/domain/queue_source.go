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

func (s QueueSource) Format() string {
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

func PackSourceId(source QueueSource, fallback string) (string, error) {
	if source.IsZero() {
		return fallback, nil
	}
	if !source.hasKnownKind() {
		return "", &ValidationError{Message: fmt.Sprintf("unknown queue source kind: %q", source.Kind)}
	}
	formatted := source.Format()
	if formatted == "" {
		return fallback, nil
	}
	return formatted, nil
}

func unescape(value string) (string, bool) {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return "", false
	}
	return decoded, true
}
