package domain

import (
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

func (s QueueSource) Format() string {
	switch s.Kind {
	case SourceKindPlaylist:
		return SourceKindPlaylist + ":" + s.PlaylistId + ":" + url.QueryEscape(s.Name)
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
		playlistId, name, hasName := strings.Cut(rest, ":")
		source := QueueSource{Kind: SourceKindPlaylist, PlaylistId: playlistId}
		if hasName {
			source.Name = unescapeOrEmpty(name)
		}
		return source
	}
	if rest, found := strings.CutPrefix(sourceId, SourceKindSearch+":"); found {
		return QueueSource{Kind: SourceKindSearch, Query: unescapeOrEmpty(rest)}
	}
	switch sourceId {
	case SourceKindLibrary:
		return QueueSource{Kind: SourceKindLibrary}
	case SourceKindSearch:
		return QueueSource{Kind: SourceKindSearch}
	}
	return QueueSource{}
}

func unescapeOrEmpty(value string) string {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return ""
	}
	return decoded
}
