package domain

import (
	"fmt"
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
		return SourceKindPlaylist + ":" + escape(s.PlaylistId) + ":" + escape(s.Name)
	case SourceKindSearch:
		if s.Query == "" {
			return SourceKindSearch
		}
		return SourceKindSearch + ":" + escape(s.Query)
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

const (
	upperHex   = "0123456789ABCDEF"
	lowerHex   = "0123456789abcdef"
	unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
)

func escape(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case strings.IndexByte(unreserved, c) >= 0:
			out.WriteByte(c)
		case c == ' ':
			out.WriteByte('+')
		default:
			out.Write([]byte{'%', upperHex[c>>4], upperHex[c&15]})
		}
	}
	return out.String()
}

func unescape(value string) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '+':
			out.WriteByte(' ')
		case '%':
			c, ok := escapedByte(value, i)
			if !ok {
				return "", false
			}
			out.WriteByte(c)
			i += 2
		default:
			out.WriteByte(value[i])
		}
	}
	return out.String(), true
}

func escapedByte(value string, i int) (byte, bool) {
	if i+2 >= len(value) {
		return 0, false
	}
	hi, lo := hexDigit(value[i+1]), hexDigit(value[i+2])
	return byte(hi<<4 | lo), hi >= 0 && lo >= 0
}

func hexDigit(c byte) int {
	if d := strings.IndexByte(upperHex, c); d >= 0 {
		return d
	}
	return strings.IndexByte(lowerHex, c)
}
