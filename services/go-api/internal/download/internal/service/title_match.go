package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
	"math"
	"slices"
	"strings"
)

type TitleKind string

const (
	TitleArtTrack    TitleKind = "art_track"
	TitlePinnedVideo TitleKind = "pinned_video"
	TitleUpload      TitleKind = "upload"
)

type TitleTarget struct {
	Title    string
	Artist   string
	Album    string
	Length   float64
	Explicit *bool
}

type TitleCandidate struct {
	URL      string
	Kind     TitleKind
	Title    string
	Artists  []string
	Album    string
	Length   float64
	Explicit bool
}

type TitleOutcome string

const (
	TitleAccept TitleOutcome = "accept"
	TitleVerify TitleOutcome = "verify"
	TitleReject TitleOutcome = "reject"
)

type TitleDecision struct {
	Outcome TitleOutcome
	Reason  string
}

func EvaluateTitle(t TitleTarget, c TitleCandidate) TitleDecision {
	if titleIdentity(c.Title) != titleIdentity(t.Title) {
		return TitleDecision{TitleReject, "different title"}
	}
	if !domain.Credits(c.Artists, t.Artist) {
		return TitleDecision{TitleReject, "artist not credited"}
	}
	if want, got := music.VersionLabels(t.Title), music.VersionLabels(c.Title); !slices.Equal(want, got) {
		return TitleDecision{TitleReject, "different version: want " + labelList(want) + ", got " + labelList(got)}
	}
	if c.Kind == TitleUpload {
		return TitleDecision{TitleVerify, "upload: needs a fingerprint match against the official track"}
	}
	switch {
	case t.Length > 0 && c.Length > 0 && !lengthsAgree(t.Length, c.Length):
		return TitleDecision{TitleReject, "length disagrees with the catalog length"}
	case t.Length <= 0 && albumKnown(t) && music.NormalizeForMatch(c.Album) == "":
		return TitleDecision{TitleVerify, "no catalog length and unknown album: cannot confirm the cut"}
	case t.Length <= 0 && albumKnown(t) && !sameAlbum(t.Album, c.Album):
		return TitleDecision{TitleReject, "no catalog length and another album: cannot confirm the cut"}
	default:
		return TitleDecision{TitleAccept, "official, same title, artist and version"}
	}
}

func ChooseTitle(t TitleTarget, candidates []TitleCandidate) (TitleCandidate, TitleDecision, bool) {
	best, bestDecision, found := TitleCandidate{}, TitleDecision{}, false
	for _, c := range candidates {
		d := EvaluateTitle(t, c)
		if d.Outcome != TitleAccept {
			continue
		}
		if !found || preferred(t, c, best) {
			best, bestDecision, found = c, d, true
		}
	}
	return best, bestDecision, found
}

func preferred(t TitleTarget, a, b TitleCandidate) bool {
	if ra, rb := kindRank(a.Kind), kindRank(b.Kind); ra != rb {
		return ra < rb
	}
	if ea, eb := exactTitle(t.Title, a.Title), exactTitle(t.Title, b.Title); ea != eb {
		return ea
	}
	if sa, sb := sameAlbum(t.Album, a.Album), sameAlbum(t.Album, b.Album); sa != sb {
		return sa
	}
	if a.Explicit != b.Explicit {
		return a.Explicit == (t.Explicit == nil || *t.Explicit)
	}
	if t.Length > 0 {
		return math.Abs(a.Length-t.Length) < math.Abs(b.Length-t.Length)
	}
	return false
}

func kindRank(k TitleKind) int {
	switch k {
	case TitlePinnedVideo:
		return 0
	case TitleArtTrack:
		return 1
	case TitleUpload:
		return 2
	}
	return 2
}

func lengthsAgree(want, got float64) bool {
	return SameRecording(want, got)
}

func titleIdentity(title string) string {
	tokens := music.TitleTokens(domain.CreditBrackets.ReplaceAllString(music.StripFeatureCredits(title), ""))
	return strings.Join(music.StripVersionPhrases(tokens), " ")
}

func exactTitle(want, got string) bool {
	return music.NormalizeForIdentity(want) == music.NormalizeForIdentity(got)
}

func albumKnown(t TitleTarget) bool {
	album := music.NormalizeForMatch(t.Album)
	return album != "" && album != music.NormalizeForMatch(t.Title)
}

func sameAlbum(want, got string) bool {
	w := music.NormalizeForMatch(want)
	return w != "" && w == music.NormalizeForMatch(got)
}

func labelList(labels []string) string {
	if len(labels) == 0 {
		return "none"
	}
	return strings.Join(labels, "+")
}
